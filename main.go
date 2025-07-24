package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func getKubeClient() (*kubernetes.Clientset, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	return clientset, err
}

func sendToPortkey(event corev1.Event) {
	apiKey := os.Getenv("PORTKEY_API_KEY")
	if apiKey == "" {
		log.Println("PORTKEY_API_KEY not set")
		return
	}

	payload := map[string]interface{}{
		"eventType":  event.Type,
		"reason":     event.Reason,
		"message":    event.Message,
		"involved":   event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name,
		"namespace":  event.Namespace,
		"timestamp":  event.FirstTimestamp,
	}

	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "https://api.portkey.ai/v1/completions", 
		bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Error creating HTTP request:", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error sending to Portkey:", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Portkey responded with status: %s\n", resp.Status)
}

func main() {
	clientset, err := getKubeClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	watch, err := clientset.CoreV1().Events("").Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Failed to watch events: %v", err)
	}

	log.Println("Watching Kubernetes events...")
	for event := range watch.ResultChan() {
		if ev, ok := event.Object.(*corev1.Event); ok {
			log.Printf("Event: %s %s %s", ev.Type, ev.Reason, ev.Message)
			sendToPortkey(*ev)
		}
	}
}
