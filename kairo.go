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
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	// Setup Kubernetes client
	kubeconfig := flag.String("kubeconfig", "", "absolute path to the kubeconfig file (optional for in-cluster)")
	flag.Parse()

	var config *rest.Config
	var err error

	if *kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalf("Failed to create Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Set up signal handler
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	watchPods(ctx, clientset)
}

func watchPods(ctx context.Context, clientset *kubernetes.Clientset) {
	watcher, err := clientset.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		Watch:         true,
	})
	if err != nil {
		log.Fatalf("Failed to start pod watcher: %v", err)
	}
	log.Println("📡 Watching for CrashLoopBackOff events...")

	for {
		select {
		case <-ctx.Done():
			log.Println("⛔️ Shutting down watcher...")
			return
		case event := <-watcher.ResultChan():
			pod, ok := event.Object.(*v1.Pod)
			if !ok {
				continue
			}

			if pod.Status.ContainerStatuses == nil {
				continue
			}

			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
					payload := map[string]interface{}{
						"name":      pod.Name,
						"namespace": pod.Namespace,
						"reason":    cs.State.Waiting.Reason,
						"message":   cs.State.Waiting.Message,
						"timestamp": time.Now().Format(time.RFC3339),
						"node":      pod.Spec.NodeName,
					}

					go sendToGateway(payload)
				}
			}
		}
	}
}

func sendToGateway(event map[string]interface{}) {
	jsonBody, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal JSON: %v", err)
		return
	}

	resp, err := http.Post("http://localhost:5000/event", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("Failed to send event to gateway: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("📨 CrashLoopBackOff sent for pod %s", event["name"])
}
