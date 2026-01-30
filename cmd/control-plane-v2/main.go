package main

import (
	"context"
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lpmos/lpmos-go/pkg/api"
	"github.com/lpmos/lpmos-go/pkg/etcd"
	"github.com/lpmos/lpmos-go/pkg/models"
	ws "github.com/lpmos/lpmos-go/pkg/websocket"
	clientv3 "go.etcd.io/etcd/client/v3"
)

//go:embed web/dashboard.html
var dashboardHTML embed.FS

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

func main() {
	log.Println("Starting LPMOS Control Plane (Full-Stack)...")

	// Get configuration from environment
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", "localhost:2379")
	apiPort := getEnv("API_PORT", "8080")

	// Create etcd client
	etcdClient, err := etcd.NewClient(etcd.Config{
		Endpoints:      []string{etcdEndpoints},
		DialTimeout:    5 * time.Second,
		RequestTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create etcd client: %v", err)
	}
	// Close the etcd client connection when the program exits
	// to release network resources and clean up goroutines
	defer etcdClient.Close()

	log.Println("Connected to etcd cluster")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create WebSocket hub
	wsHub := ws.NewHub()
	go wsHub.Run()

	// Start task watcher in background
	handler := api.NewHandler(etcdClient)
	go handler.StartTaskWatcher(ctx)

	// Start progress watcher for WebSocket broadcasting
	go startProgressWatcher(ctx, etcdClient, wsHub)

	// Setup HTTP router with WebSocket and frontend
	router := setupFullStackRouter(etcdClient, wsHub)

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down gracefully...")
		cancel()
		time.Sleep(time.Second)
		os.Exit(0)
	}()

	// Start HTTP server
	addr := ":" + apiPort
	log.Printf("Control Plane Full-Stack Server listening on %s", addr)
	log.Printf("Dashboard: http://localhost:%s/", apiPort)
	log.Printf("API:       http://localhost:%s/api/v1/", apiPort)
	log.Printf("WebSocket: ws://localhost:%s/ws", apiPort)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func setupFullStackRouter(etcdClient *etcd.Client, wsHub *ws.Hub) *gin.Engine {
	router := gin.Default()

	handler := api.NewHandler(etcdClient)

	// Frontend routes
	router.GET("/", serveDashboard)
	router.GET("/dashboard", serveDashboard)

	// WebSocket endpoint
	router.GET("/ws", func(c *gin.Context) {
		handleWebSocket(c.Writer, c.Request, wsHub)
	})

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Task management
		v1.POST("/tasks", handler.CreateTask)
		v1.GET("/tasks", handler.ListTasks)
		v1.GET("/tasks/:id", handler.GetTask)
		v1.PUT("/tasks/:id/approve", handler.ApproveTask)
		v1.DELETE("/tasks/:id", handler.DeleteTask)

		// Health check
		v1.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "healthy",
				"service": "control-plane-fullstack",
			})
		})
	}

	return router
}

func serveDashboard(c *gin.Context) {
	// Read embedded HTML file
	htmlContent, err := dashboardHTML.ReadFile("web/dashboard.html")
	if err != nil {
		// Try direct file system read for development
		htmlContent, err = os.ReadFile("web/dashboard.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load dashboard")
			return
		}
	}

	tmpl, err := template.New("dashboard").Parse(string(htmlContent))
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to parse template")
		return
	}

	c.Header("Content-Type", "text/html")
	tmpl.Execute(c.Writer, nil)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, hub *ws.Hub) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return
	}

	client := &ws.Client{
		Hub:  hub,
		Conn: conn,
		Send: make(chan []byte, 256),
	}

	client.Hub.Register <- client

	// Start goroutines for reading and writing
	go client.WritePump()
	go client.ReadPump()
}

func startProgressWatcher(ctx context.Context, etcdClient *etcd.Client, wsHub *ws.Hub) {
	watchChan := etcdClient.Watch(ctx, etcd.KeyPrefixTasks, true)

	log.Println("Control plane: started watching for progress updates")

	for {
		select {
		case <-ctx.Done():
			log.Println("Control plane: stopping progress watcher")
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				log.Printf("Watch error: %v", watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				if event.Type != clientv3.EventTypePut {
					continue
				}

				key := string(event.Kv.Key)

				// Handle progress updates
				if strings.HasSuffix(key, "/progress") {
					taskID := extractTaskID(key)
					if taskID == "" {
						continue
					}

					var progress models.Progress
					if err := json.Unmarshal(event.Kv.Value, &progress); err != nil {
						log.Printf("Failed to unmarshal progress: %v", err)
						continue
					}

					log.Printf("Progress update for task %s: %d%% (%s)", taskID, progress.Percentage, progress.Stage)
					wsHub.BroadcastProgress(taskID, &progress)
				}

				// Handle status changes
				if strings.HasSuffix(key, "/status") {
					taskID := extractTaskID(key)
					if taskID == "" {
						continue
					}

					status := models.TaskStatus(event.Kv.Value)
					log.Printf("Status update for task %s: %s", taskID, status)
					wsHub.BroadcastStatus(taskID, status)
				}

				// Handle hardware reports
				if strings.HasSuffix(key, "/hardware") {
					taskID := extractTaskID(key)
					if taskID == "" {
						continue
					}

					var hardware models.HardwareInfo
					if err := json.Unmarshal(event.Kv.Value, &hardware); err != nil {
						log.Printf("Failed to unmarshal hardware: %v", err)
						continue
					}

					log.Printf("Hardware report for task %s: %s", taskID, hardware.MACAddress)
					wsHub.BroadcastHardware(taskID, &hardware)
				}
			}
		}
	}
}

func extractTaskID(key string) string {
	// Extract task ID from keys like /lpmos/tasks/{task_id}/progress
	parts := strings.Split(key, "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
