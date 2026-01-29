package main

import (
	"context"
	"encoding/json"
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
	regionID   string
	etcdClient *etcd.Client
	ctx        context.Context
	cancel     context.CancelFunc
}

func main() {
	log.Println("Starting LPMOS Regional Client...")

	// Get configuration from environment
	regionID := getEnv("REGION_ID", "dc1")
	etcdEndpoints := getEnv("ETCD_ENDPOINTS", "localhost:2379")
	apiPort := getEnv("API_PORT", "8081")

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

	log.Printf("Regional Client [%s] connected to etcd cluster", regionID)

	// Create regional client
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &RegionalClient{
		regionID:   regionID,
		etcdClient: etcdClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start heartbeat
	go client.startHeartbeat()

	// Start watching for tasks
	go client.watchTasks()

	// Start API server for agent communication
	go client.startAPIServer(apiPort)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
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
			heartbeat := models.RegionalClientHeartbeat{
				RegionID:      c.regionID,
				Status:        "online",
				Services: map[string]string{
					"dhcp": "running",
					"tftp": "running",
					"http": "running",
				},
				EtcdConnected: true,
				LastHeartbeat: time.Now(),
			}

			// Store heartbeat with TTL
			key := etcd.RegionKey(c.regionID, "client", "heartbeat")
			if err := c.etcdClient.PutWithLease(key, heartbeat, 30); err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
			} else {
				log.Printf("[%s] Heartbeat sent", c.regionID)
			}
		}
	}
}

func (c *RegionalClient) watchTasks() {
	// Watch for task assignments in this region
	watchKey := etcd.RegionKey(c.regionID, "tasks")
	watchChan := c.etcdClient.Watch(c.ctx, watchKey, true)

	log.Printf("[%s] Watching for task assignments: %s", c.regionID, watchKey)

	for {
		select {
		case <-c.ctx.Done():
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				log.Printf("Watch error: %v", watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				if event.Type == clientv3.EventTypePut {
					// Extract task ID from key
					key := string(event.Kv.Key)
					parts := strings.Split(key, "/")
					if len(parts) > 0 {
						taskID := parts[len(parts)-1]
						log.Printf("[%s] New task assigned: %s", c.regionID, taskID)
						go c.handleTask(taskID)
					}
				}
			}
		}
	}
}

func (c *RegionalClient) handleTask(taskID string) {
	log.Printf("[%s] Processing task %s", c.regionID, taskID)

	// Load task metadata
	var task models.Task
	if err := c.etcdClient.GetJSON(etcd.TaskKey(taskID, "metadata"), &task); err != nil {
		log.Printf("Failed to load task %s: %v", taskID, err)
		return
	}

	// Update status to ready
	if err := c.etcdClient.Put(etcd.TaskKey(taskID, "status"), string(models.TaskStatusReady)); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return
	}

	log.Printf("[%s] Task %s ready for PXE boot (target MAC: %s)", c.regionID, taskID, task.TargetMAC)

	// In production, this would:
	// 1. Configure DHCP to serve PXE boot for the target MAC
	// 2. Prepare TFTP/HTTP files for iPXE boot
	// 3. Generate preseed/kickstart configuration
	// 4. Wait for agent to report hardware

	// Watch for approval
	go c.watchTaskApproval(taskID)
}

func (c *RegionalClient) watchTaskApproval(taskID string) {
	watchKey := etcd.TaskKey(taskID, "approval")
	watchChan := c.etcdClient.Watch(c.ctx, watchKey, false)

	log.Printf("[%s] Watching for approval: %s", c.regionID, taskID)

	for {
		select {
		case <-c.ctx.Done():
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				log.Printf("Watch error: %v", watchResp.Err())
				return
			}

			for _, event := range watchResp.Events {
				if event.Type == clientv3.EventTypePut {
					var approval models.Approval
					if err := json.Unmarshal(event.Kv.Value, &approval); err != nil {
						log.Printf("Failed to unmarshal approval: %v", err)
						continue
					}

					if approval.Status == models.ApprovalStatusApproved {
						log.Printf("[%s] Task %s approved! Starting OS installation...", c.regionID, taskID)
						go c.installOS(taskID)
						return
					} else if approval.Status == models.ApprovalStatusRejected {
						log.Printf("[%s] Task %s rejected: %s", c.regionID, taskID, approval.Reason)
						return
					}
				}
			}
		}
	}
}

func (c *RegionalClient) installOS(taskID string) {
	// Update status to installing
	if err := c.etcdClient.Put(etcd.TaskKey(taskID, "status"), string(models.TaskStatusInstalling)); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return
	}

	log.Printf("[%s] Installing OS for task %s...", c.regionID, taskID)

	// Simulate installation process
	time.Sleep(5 * time.Second)

	// In production, this would:
	// 1. Configure PXE to boot into installation mode
	// 2. Serve OS installation images
	// 3. Monitor installation progress
	// 4. Verify installation completion

	// Update status to completed
	if err := c.etcdClient.Put(etcd.TaskKey(taskID, "status"), string(models.TaskStatusCompleted)); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return
	}

	log.Printf("[%s] Task %s completed successfully!", c.regionID, taskID)
}

func (c *RegionalClient) startAPIServer(port string) {
	router := gin.Default()

	// Agent report endpoint
	router.POST("/api/v1/agent/report", func(ctx *gin.Context) {
		var req models.AgentReportRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[%s] Received hardware report from agent: %s", c.regionID, req.MACAddress)

		// Find task by MAC address
		taskID, err := c.findTaskByMAC(req.MACAddress)
		if err != nil {
			log.Printf("Failed to find task for MAC %s: %v", req.MACAddress, err)
			ctx.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}

		// Store hardware info
		if err := c.etcdClient.Put(etcd.TaskKey(taskID, "hardware"), req.Hardware); err != nil {
			log.Printf("Failed to store hardware info: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store hardware"})
			return
		}

		// Update task status to pending_approval
		if err := c.etcdClient.Put(etcd.TaskKey(taskID, "status"), string(models.TaskStatusPendingApproval)); err != nil {
			log.Printf("Failed to update task status: %v", err)
		}

		log.Printf("[%s] Hardware report stored for task %s", c.regionID, taskID)

		ctx.JSON(http.StatusOK, gin.H{
			"status":      "received",
			"task_id":     taskID,
			"next_action": "wait_for_approval",
		})
	})

	// Agent status update endpoint
	router.POST("/api/v1/agent/status", func(ctx *gin.Context) {
		var req models.AgentStatusRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[%s] Agent status update for task %s: %s (%d%%)", c.regionID, req.TaskID, req.Status, req.Progress)
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Health check
	router.GET("/api/v1/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"status":         "healthy",
			"region_id":      c.regionID,
			"services": gin.H{
				"dhcp": "running",
				"tftp": "running",
				"http": "running",
			},
			"etcd_connected": true,
		})
	})

	addr := ":" + port
	log.Printf("[%s] Regional Client API listening on %s", c.regionID, addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}
}

func (c *RegionalClient) findTaskByMAC(macAddr string) (string, error) {
	// Search all tasks in this region for matching MAC
	kvs, err := c.etcdClient.GetWithPrefix(etcd.KeyPrefixTasks)
	if err != nil {
		return "", err
	}

	for key, value := range kvs {
		if !strings.HasSuffix(key, "/metadata") {
			continue
		}

		var task models.Task
		if err := json.Unmarshal(value, &task); err != nil {
			continue
		}

		if task.RegionID == c.regionID && strings.EqualFold(task.TargetMAC, macAddr) {
			return task.ID, nil
		}
	}

	return "", fmt.Errorf("no task found for MAC address %s in region %s", macAddr, c.regionID)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
