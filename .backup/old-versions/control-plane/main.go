package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lpmos/lpmos-go/pkg/api"
	"github.com/lpmos/lpmos-go/pkg/etcd"
)

func main() {
	log.Println("Starting LPMOS Control Plane...")

	// Get etcd configuration from environment or use defaults
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

	// Start task watcher in background
	handler := api.NewHandler(etcdClient)
	go handler.StartTaskWatcher(ctx)

	// Setup API router
	router := api.SetupRouter(etcdClient)

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down gracefully...")
		cancel()
		os.Exit(0)
	}()

	// Start HTTP server
	addr := ":" + apiPort
	log.Printf("Control Plane API listening on %s", addr)
	log.Printf("Example: curl http://localhost:%s/api/v1/health", apiPort)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
