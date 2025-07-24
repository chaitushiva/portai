// File: main.go
package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/watch"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/tools/clientcmd"
)

type EventPayload struct {
    PodName   string `json:"podName"`
    NodeName  string `json:"nodeName"`
    EventType string `json:"eventType"`
    Reason    string `json:"reason"`
    Message   string `json:"message"`
    Timestamp string `json:"timestamp"`
}

func getKubernetesClient() (*kubernetes.Clientset, error) {
    var config *rest.Config
    var err error

    if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
        config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
    } else {
        config, err = rest.InClusterConfig()
    }
    if err != nil {
        return nil, err
    }

    return kubernetes.NewForConfig(config)
}

func callPortkeyAI(event EventPayload) {
    systemPrompt := "You are an SRE assistant. Given a Kubernetes event, return the root cause and suggested action."
    content := fmt.Sprintf("Pod: %s\nNode: %s\nType: %s\nReason: %s\nMessage: %s\nTimestamp: %s",
        event.PodName, event.NodeName, event.EventType, event.Reason, event.Message, event.Timestamp)

    payload := map[string]interface{}{
        "model": "gpt-4",
        "messages": []map[string]string{
            {"role": "system", "content": systemPrompt},
            {"role": "user", "content": content},
        },
    }

    jsonBody, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", "https://api.portkey.ai/v1/chat/completions", bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+os.Getenv("PORTKEY_API_KEY"))
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Printf("[ERROR] Failed to call Portkey AI: %v", err)
        return
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    fmt.Printf("[AI RESPONSE] %+v\n", result)
}

func main() {
    clientset, err := getKubernetesClient()
    if err != nil {
        log.Fatalf("[ERROR] Failed to create K8s client: %v", err)
    }

    watcher, err := clientset.CoreV1().Events("").Watch(context.TODO(), metav1.ListOptions{})
    if err != nil {
        log.Fatalf("[ERROR] Failed to set up event watcher: %v", err)
    }

    log.Println("[INFO] Watching Kubernetes events...")
    for event := range watcher.ResultChan() {
        if evt, ok := event.Object.(*metav1.Event); ok {
            if evt.Type == "Warning" || evt.Reason == "CrashLoopBackOff" {
                payload := EventPayload{
                    PodName:   evt.InvolvedObject.Name,
                    NodeName:  evt.Source.Host,
                    EventType: evt.Type,
                    Reason:    evt.Reason,
                    Message:   evt.Message,
                    Timestamp: evt.LastTimestamp.Format(time.RFC3339),
                }
                go callPortkeyAI(payload)
            }
        }
    }
}
