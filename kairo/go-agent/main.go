package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	useRedis       = os.Getenv("USE_REDIS") == "true"
	portkeyAPIURL  = os.Getenv("PORTKEY_API_URL")
	redisHost      = os.Getenv("REDIS_HOST")
	redisPort      = os.Getenv("REDIS_PORT")
	cacheTTL       = getCacheTTL()
	failureReasons = []string{"CrashLoopBackOff", "Error", "OOMKilled"}
)

var (
	rdb   *redis.Client
	memMu sync.Mutex
	mem   = make(map[string]time.Time)
)

func getCacheTTL() time.Duration {
	val := os.Getenv("CACHE_TTL_SECONDS")
	if val == "" {
		return 300 * time.Second // default 5 mins
	}
	ttl, err := time.ParseDuration(val + "s")
	if err != nil {
		return 300 * time.Second
	}
	return ttl
}

func setupRedis() {
	if !useRedis {
		log.Println("Redis disabled, using in-memory cache.")
		return
	}
	rdb = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis.")
}

func cacheKey(ns, name string) string {
	return fmt.Sprintf("pod-event:%s:%s", ns, name)
}

func isCached(namespace, name string) bool {
	key := cacheKey(namespace, name)
	if useRedis {
		exists, err := rdb.Exists(context.Background(), key).Result()
		return err == nil && exists > 0
	}
	memMu.Lock()
	defer memMu.Unlock()
	exp, found := mem[key]
	if !found {
		return false
	}
	if time.Now().After(exp) {
		delete(mem, key)
		return false
	}
	return true
}

func cache(namespace, name string) {
	key := cacheKey(namespace, name)
	if useRedis {
		rdb.Set(context.Background(), key, "1", cacheTTL)
	} else {
		memMu.Lock()
		defer memMu.Unlock()
		mem[key] = time.Now().Add(cacheTTL)
	}
}

func postToPythonAPI(namespace, name, reason string) {
	if portkeyAPIURL == "" {
		log.Println("PORTKEY_API_URL not set")
		return
	}

	payload := map[string]string{
		"namespace": namespace,
		"pod":       name,
		"reason":    reason,
	}
	jsonData, _ := json.Marshal(payload)

	resp, err := http.Post(portkeyAPIURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error sending to Python API: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Sent event to Python API: %s/%s (%s) → Status: %d", namespace, name, reason, resp.StatusCode)
}

func shouldPost(reason string) bool {
	for _, r := range failureReasons {
		if strings.Contains(reason, r) {
			return true
		}
	}
	return false
}

func getKubeClient() (*kubernetes.Clientset, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}

func main() {
	log.Println("Kairo Agent starting up...")
	setupRedis()

	clientset, err := getKubeClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	watcher, err := clientset.CoreV1().Pods("").Watch(context.TODO(), v1.ListOptions{
		Watch: true,
	})
	if err != nil {
		log.Fatalf("Failed to watch pods: %v", err)
	}
	log.Println("Started watching Pod events...")

	for event := range watcher.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		ns := pod.Namespace
		name := pod.Name

		if isCached(ns, name) {
			continue
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && shouldPost(cs.State.Waiting.Reason) {
				log.Printf("Event matched: %s/%s → %s", ns, name, cs.State.Waiting.Reason)
				cache(ns, name)
				postToPythonAPI(ns, name, cs.State.Waiting.Reason)
				break
			}
		}
	}
}
