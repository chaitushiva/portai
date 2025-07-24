package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	config, err := loadKubeConfig()
	if err != nil {
		log.Fatalf("❌ Failed to create Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("❌ Failed to create Kubernetes client: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	watchPods(ctx, clientset)
}

func loadKubeConfig() (*rest.Config, error) {
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		log.Printf("📁 Using kubeconfig from KUBECONFIG=%s", kubeconfigEnv)
		return clientcmd.BuildConfigFromFlags("", kubeconfigEnv)
	}
	log.Println("📦 Using in-cluster Kubernetes config")
	return rest.InClusterConfig()
}

func watchPods(ctx context.Context, clientset *kubernetes.Clientset) {
	watcher, err := clientset.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		Watch:         true,
	})
	if err != nil {
		log.Fatalf("❌ Failed to start pod watcher: %v", err)
	}
	log.Println("📡 Watching for CrashLoopBackOff and Pending pod events...")

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Shutting down watcher...")
			return
		case event := <-watcher.ResultChan():
			pod, ok := event.Object.(*v1.Pod)
			if !ok {
				continue
			}

			// Filter for Pending phase
			if pod.Status.Phase == v1.PodPending {
				sendEvent(pod, "Pending", "Pod is stuck in Pending phase")
			}

			// Filter for CrashLoopBackOff
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
					sendEvent(pod, "CrashLoopBackOff", cs.State.Waiting.Message)
				}
			}
		}
	}
}

func sendEvent(pod *v1.Pod, reason, message string) {
	payload := map[string]interface{}{
		"name":      pod.Name,
		"namespace": pod.Namespace,
		"reason":    reason,
		"message":   message,
		"timestamp": time.Now().Format(time.RFC3339),
		"node":      pod.Spec.NodeName,
	}

	go sendToGateway(payload)
}

func sendToGateway(event map[string]interface{}) {
	jsonBody, err := json.Marshal(event)
	if err != nil {
		log.Printf("❌ Failed to marshal event JSON: %v", err)
		return
	}

	// Use localhost ONLY if the gateway is running locally.
	url := os.Getenv("GATEWAY_URL")
	if url == "" {
		url = "http://localhost:5000/event"
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("❌ Failed to send event to gateway: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("📨 Sent %s event for pod %s in namespace %s", event["reason"], event["name"], event["namespace"])
}
