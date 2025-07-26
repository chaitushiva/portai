package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type PodFailureEvent struct {
	PodName   string `json:"pod_name"`
	Namespace string `json:"namespace"`
	Reason    string `json:"reason"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp"`
}

func main() {
	apiURL := flag.String("api-url", "http://localhost:8000/analyze", "Kairo API URL")
	flag.Parse()

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes clientset: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Println("Starting Kairo Agent pod watcher...")

	watchPods(ctx, clientset, *apiURL)
}

func loadConfig() (*rest.Config, error) {
	kubeconfigEnv := os.Getenv("KUBECONFIG")
	if kubeconfigEnv != "" {
		log.Printf("Using kubeconfig from KUBECONFIG: %s", kubeconfigEnv)
		return clientcmd.BuildConfigFromFlags("", kubeconfigEnv)
	}

	log.Println("Using in-cluster kubeconfig")
	return rest.InClusterConfig()
}

func watchPods(ctx context.Context, clientset *kubernetes.Clientset, apiURL string) {
	// Cache to store last event time per pod (namespace/podName)
	eventCache := make(map[string]time.Time)
	cacheTTL := 10 * time.Minute

	watcher, err := clientset.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		Watch:         true,
	})
	if err != nil {
		log.Fatalf("Failed to create pod watcher: %v", err)
	}
	defer watcher.Stop()

	log.Println("Watching pods for CrashLoopBackOff...")

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down pod watcher...")
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				log.Println("Watcher channel closed, restarting watcher...")
				// restart logic omitted for brevity
				return
			}
			pod, ok := event.Object.(*v1.Pod)
			if !ok || pod.Status.ContainerStatuses == nil {
				continue
			}

			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
					key := pod.Namespace + "/" + pod.Name
					now := time.Now()

					// Check cache to avoid duplicates
					if lastSent, found := eventCache[key]; found {
						if now.Sub(lastSent) < cacheTTL {
							log.Printf("Skipping duplicate event for %s (last sent %v ago)", key, now.Sub(lastSent))
							continue
						}
					}

					log.Printf("Pod %s in CrashLoopBackOff, sending event...", key)
					eventCache[key] = now

					event := PodFailureEvent{
						PodName:   pod.Name,
						Namespace: pod.Namespace,
						Reason:    cs.State.Waiting.Reason,
						Message:   cs.State.Waiting.Message,
						Timestamp: now.UTC().Format(time.RFC3339),
					}
					go sendEvent(apiURL, event)
				}
			}
		}
	}
}

func sendEvent(apiURL string, event PodFailureEvent) {
	jsonBytes, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event JSON: %v", err)
		return
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		log.Printf("Failed to POST event to API: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Received non-OK response from API: %v", resp.Status)
		return
	}

	log.Printf("Successfully sent event for pod %s/%s", event.Namespace, event.PodName)
}
