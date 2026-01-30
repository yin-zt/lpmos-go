package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lpmos/lpmos-go/pkg/etcd"
	"github.com/lpmos/lpmos-go/pkg/models"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type RegionalClient struct {
	label      string
	etcdClient *etcd.Client
	ctx        context.Context
	cancel     context.CancelFunc
}

var (
	regionLabel    = flag.String("label", getEnv("REGION_LABEL", "dc1"), "Region label (e.g., dc1, dc2)")
	etcdEndpoints  = flag.String("etcd-endpoints", getEnv("ETCD_ENDPOINTS", "localhost:2379"), "etcd endpoints")
	apiPort        = flag.String("api-port", getEnv("API_PORT", "8081"), "API port")
)

func main() {
	flag.Parse()

	log.Printf("Starting LPMOS Regional Client [Label: %s]...", *regionLabel)

	// Create etcd client
	etcdClient, err := etcd.NewClient(etcd.Config{
		Endpoints:      []string{*etcdEndpoints},
		DialTimeout:    5 * time.Second,
		RequestTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create etcd client: %v", err)
	}
	// Close the etcd client connection when the program exits
	// to release network resources and clean up goroutines
	defer etcdClient.Close()

	log.Printf("[%s] Connected to etcd cluster", *regionLabel)

	// Create regional client
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &RegionalClient{
		label:      *regionLabel,
		etcdClient: etcdClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start heartbeat
	go client.startHeartbeat()

	// Start watching for tasks in this region
	go client.watchTasks()

	// Start API server for agent communication
	go client.startAPIServer(*apiPort)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("[%s] Shutting down gracefully...", *regionLabel)
	cancel()
	time.Sleep(1 * time.Second)
}

func (c *RegionalClient) startHeartbeat() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			timestamp := time.Now().Format(time.RFC3339)

			// Store heartbeat with TTL
			key := etcd.RegionKey(c.label, "heartbeat")
			if err := c.etcdClient.PutWithLease(key, timestamp, 30); err != nil {
				log.Printf("[%s] Failed to send heartbeat: %v", c.label, err)
			} else {
				log.Printf("[%s] Heartbeat sent", c.label)
			}

			// Update status
			statusKey := etcd.RegionKey(c.label, "status")
			c.etcdClient.Put(statusKey, "online")
		}
	}
}

func (c *RegionalClient) watchTasks() {
	// Watch for task assignments in this region
	watchKey := etcd.TaskKeyPrefix(c.label)
	watchChan := c.etcdClient.Watch(c.ctx, watchKey, true)

	log.Printf("[%s] Watching for task assignments: %s", c.label, watchKey)

	for {
		select {
		case <-c.ctx.Done():
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				log.Printf("[%s] Watch error: %v", c.label, watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				if event.Type == clientv3.EventTypePut {
					// Extract task ID from key
					key := string(event.Kv.Key)
					if strings.HasSuffix(key, "/metadata") {
						taskID := c.extractTaskID(key)
						if taskID != "" {
							log.Printf("[%s] New task assigned: %s", c.label, taskID)
							go c.handleTask(taskID)
						}
					}
				}
			}
		}
	}
}

func (c *RegionalClient) handleTask(taskID string) {
	log.Printf("[%s] Processing task %s", c.label, taskID)

	// Load task metadata
	var task models.Task
	if err := c.etcdClient.GetJSON(etcd.TaskKey(c.label, taskID, "metadata"), &task); err != nil {
		log.Printf("[%s] Failed to load task %s: %v", c.label, taskID, err)
		return
	}

	// Update status to ready
	if err := c.etcdClient.Put(etcd.TaskKey(c.label, taskID, "status"), string(models.TaskStatusReady)); err != nil {
		log.Printf("[%s] Failed to update task status: %v", c.label, err)
		return
	}

	log.Printf("[%s] Task %s ready for PXE boot (target MAC: %s)", c.label, taskID, task.TargetMAC)

	// Watch for approval
	go c.watchTaskApproval(taskID)
}

func (c *RegionalClient) watchTaskApproval(taskID string) {
	watchKey := etcd.TaskKey(c.label, taskID, "approval")
	watchChan := c.etcdClient.Watch(c.ctx, watchKey, false)

	log.Printf("[%s] Watching for approval: %s", c.label, taskID)

	for {
		select {
		case <-c.ctx.Done():
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				log.Printf("[%s] Watch error: %v", c.label, watchResp.Err())
				return
			}

			for _, event := range watchResp.Events {
				if event.Type == clientv3.EventTypePut {
					var approval models.Approval
					if err := json.Unmarshal(event.Kv.Value, &approval); err != nil {
						log.Printf("[%s] Failed to unmarshal approval: %v", c.label, err)
						continue
					}

					if approval.Status == models.ApprovalStatusApproved {
						log.Printf("[%s] Task %s approved! Starting OS installation...", c.label, taskID)
						go c.installOS(taskID)
						return
					} else if approval.Status == models.ApprovalStatusRejected {
						log.Printf("[%s] Task %s rejected: %s", c.label, taskID, approval.Reason)
						return
					}
				}
			}
		}
	}
}

func (c *RegionalClient) installOS(taskID string) {
	// Update status to installing
	if err := c.etcdClient.Put(etcd.TaskKey(c.label, taskID, "status"), string(models.TaskStatusInstalling)); err != nil {
		log.Printf("[%s] Failed to update task status: %v", c.label, err)
		return
	}

	log.Printf("[%s] Installing OS for task %s...", c.label, taskID)

	// Simulate installation process
	time.Sleep(5 * time.Second)

	// Update status to completed
	if err := c.etcdClient.Put(etcd.TaskKey(c.label, taskID, "status"), string(models.TaskStatusCompleted)); err != nil {
		log.Printf("[%s] Failed to update task status: %v", c.label, err)
		return
	}

	log.Printf("[%s] Task %s completed successfully!", c.label, taskID)
}

func (c *RegionalClient) startAPIServer(port string) {
	router := gin.Default()

	// Agent hardware report endpoint (FIXED)
	router.POST("/api/report", func(ctx *gin.Context) {
		var req models.AgentReportRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[%s] Received hardware report from agent: %s", c.label, req.MACAddress)

		// FIXED: Find task by MAC address in this region
		taskID, err := c.findTaskByMAC(req.MACAddress)
		if err != nil {
			// Log error with region label
			log.Printf("[%s] Failed to find task for MAC %s: %v", c.label, req.MACAddress, err)

			// Store unmatched report
			timestamp := time.Now().Format("20060102150405")
			identifier := fmt.Sprintf("%s-%s", timestamp, req.MACAddress)
			unmatchedKey := etcd.UnmatchedReportKey(c.label, identifier)

			unmatchedData := map[string]interface{}{
				"mac_address":  req.MACAddress,
				"region":       c.label,
				"hardware":     req.Hardware,
				"received_at":  time.Now().Format(time.RFC3339),
				"error":        fmt.Sprintf("No task found for this MAC in region %s", c.label),
			}

			if err := c.etcdClient.Put(unmatchedKey, unmatchedData); err != nil {
				log.Printf("[%s] Failed to store unmatched report: %v", c.label, err)
			}

			// Return error to agent with retry instructions
			ctx.JSON(http.StatusNotFound, gin.H{
				"status":      "error",
				"message":     fmt.Sprintf("No task found for MAC %s in region %s", req.MACAddress, c.label),
				"retry_after": 10,
			})
			return
		}

		// Store hardware info in etcd
		if err := c.etcdClient.Put(etcd.TaskKey(c.label, taskID, "hardware"), req.Hardware); err != nil {
			log.Printf("[%s] Failed to store hardware info: %v", c.label, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store hardware"})
			return
		}

		// Update task status to pending_approval
		if err := c.etcdClient.Put(etcd.TaskKey(c.label, taskID, "status"), string(models.TaskStatusPendingApproval)); err != nil {
			log.Printf("[%s] Failed to update task status: %v", c.label, err)
		}

		log.Printf("[%s] Hardware report stored for task %s", c.label, taskID)

		ctx.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"task_id": taskID,
			"message": "Hardware report received and matched to task",
		})
	})

	// Agent progress update endpoint
	router.POST("/api/progress", func(ctx *gin.Context) {
		var req models.AgentProgressRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[%s] Agent progress for task %s: [%d%%] %s - %s",
			c.label, req.TaskID, req.Percentage, req.Stage, req.Message)

		// Store progress in etcd
		progress := models.Progress{
			TaskID:     req.TaskID,
			Stage:      req.Stage,
			Percentage: req.Percentage,
			Message:    req.Message,
			Details:    req.Details,
			UpdatedAt:  time.Now(),
		}

		if err := c.etcdClient.Put(etcd.TaskKey(c.label, req.TaskID, "progress"), progress); err != nil {
			log.Printf("[%s] Failed to store progress: %v", c.label, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store progress"})
			return
		}

		// Update task status based on stage
		if req.Stage == "partitioning" || req.Stage == "downloading" ||
			req.Stage == "installing" || req.Stage == "configuring" {
			c.etcdClient.Put(etcd.TaskKey(c.label, req.TaskID, "status"), string(models.TaskStatusInstalling))
		}

		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Agent approval check endpoint
	router.GET("/api/approval/:mac", func(ctx *gin.Context) {
		macAddr := ctx.Param("mac")

		// Find task by MAC address
		taskID, err := c.findTaskByMAC(macAddr)
		if err != nil {
			ctx.JSON(http.StatusOK, gin.H{"approved": false})
			return
		}

		// Check approval status
		var approval models.Approval
		if err := c.etcdClient.GetJSON(etcd.TaskKey(c.label, taskID, "approval"), &approval); err != nil {
			ctx.JSON(http.StatusOK, gin.H{"approved": false, "task_id": taskID})
			return
		}

		// If approved, also return install config
		if approval.Status == models.ApprovalStatusApproved {
			var task models.Task
			c.etcdClient.GetJSON(etcd.TaskKey(c.label, taskID, "metadata"), &task)

			ctx.JSON(http.StatusOK, gin.H{
				"approved": true,
				"task_id":  taskID,
				"install_config": gin.H{
					"os_type":    task.OSType,
					"os_version": task.OSVersion,
				},
			})
		} else {
			ctx.JSON(http.StatusOK, gin.H{
				"approved": false,
				"task_id":  taskID,
				"status":   string(approval.Status),
			})
		}
	})

	// Health check
	router.GET("/api/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"status":         "healthy",
			"region_label":   c.label,
			"services": gin.H{
				"dhcp": "running",
				"tftp": "running",
				"http": "running",
			},
			"etcd_connected": true,
		})
	})

	addr := ":" + port
	log.Printf("[%s] Regional Client API listening on %s", c.label, addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("[%s] Failed to start API server: %v", c.label, err)
	}
}

// findTaskByMAC searches for a task in this region by target MAC address
func (c *RegionalClient) findTaskByMAC(macAddr string) (string, error) {
	// Normalize MAC address (lowercase, no separators for comparison)
	macNormalized := strings.ToLower(strings.ReplaceAll(macAddr, ":", ""))

	// Search all tasks in this region
	prefix := etcd.TaskKeyPrefix(c.label)
	kvs, err := c.etcdClient.GetWithPrefix(prefix)
	if err != nil {
		return "", fmt.Errorf("failed to search tasks: %w", err)
	}

	for key, value := range kvs {
		// Only process metadata keys
		if !strings.HasSuffix(key, "/metadata") {
			continue
		}

		var task models.Task
		if err := json.Unmarshal(value, &task); err != nil {
			log.Printf("[%s] Warning: failed to unmarshal task metadata: %v", c.label, err)
			continue
		}

		// Compare MAC addresses (case-insensitive, ignore separators)
		taskMACNormalized := strings.ToLower(strings.ReplaceAll(task.TargetMAC, ":", ""))

		if taskMACNormalized == macNormalized {
			log.Printf("[%s] Found matching task %s for MAC %s", c.label, task.ID, macAddr)
			return task.ID, nil
		}
	}

	return "", fmt.Errorf("no task found for MAC address %s in region %s", macAddr, c.label)
}

// extractTaskID extracts task ID from etcd key
// Example: /os/install/task/dc1/550e8400-.../metadata -> 550e8400-...
func (c *RegionalClient) extractTaskID(key string) string {
	parts := strings.Split(key, "/")
	// Key format: /os/install/task/{region}/{task_id}/metadata
	if len(parts) >= 6 {
		return parts[5]
	}
	return ""
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
