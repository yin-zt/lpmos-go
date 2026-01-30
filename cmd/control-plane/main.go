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
	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/lpmos/lpmos-go/pkg/etcd"
	"github.com/lpmos/lpmos-go/pkg/models"
	"github.com/lpmos/lpmos-go/pkg/websocket"
)

// ControlPlane manages the central control plane for LPMOS v3.0
type ControlPlane struct {
	etcdClient *etcd.Client
	wsHub      *websocket.Hub
	ctx        context.Context
	cancel     context.CancelFunc
}

func main() {
	log.Println("Starting LPMOS Control Plane v3.0...")

	// Initialize etcd client
	etcdClient, err := etcd.NewClient(etcd.Config{
		Endpoints:      []string{"localhost:2379"},
		DialTimeout:    5 * time.Second,
		RequestTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer etcdClient.Close() // Release etcd connection when done

	// Initialize WebSocket hub
	wsHub := websocket.NewHub()
	go wsHub.Run()

	// Create control plane
	ctx, cancel := context.WithCancel(context.Background())
	cp := &ControlPlane{
		etcdClient: etcdClient,
		wsHub:      wsHub,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start watchers
	go cp.watchTasks()
	go cp.watchLeases()

	// Setup HTTP server
	router := setupRouter(cp)

	// Start server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Println("Control plane listening on :8080")
		log.Println("Dashboard: http://localhost:8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down control plane...")
	cancel()
	srv.Shutdown(context.Background())
}

func setupRouter(cp *ControlPlane) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// WebSocket endpoint
	router.GET("/ws", func(c *gin.Context) {
		websocket.ServeWs(cp.wsHub, c.Writer, c.Request)
	})

	// API routes
	api := router.Group("/api/v1")
	{
		api.POST("/tasks", cp.createTask)
		api.GET("/tasks", cp.listTasks)
		api.GET("/tasks/:idc/:sn", cp.getTask)
		api.POST("/tasks/:idc/:sn/approve", cp.approveTask)
		api.POST("/tasks/:idc/:sn/reject", cp.rejectTask)
		api.GET("/servers/:idc", cp.listServers)
		api.GET("/stats/:idc", cp.getStats)
		api.GET("/stats", cp.getAllStats)
	}

	// Serve static files from web/index.html
	router.GET("/", func(c *gin.Context) {
		c.File("web/index.html")
	})

	return router
}

// createTask creates a new installation task using OPTIMIZED SCHEMA v3.0
func (cp *ControlPlane) createTask(c *gin.Context) {
	var req models.CreateTaskRequestV3
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Step 1: Add to servers directory (INDIVIDUAL KEY)
	serverKey := etcd.ServerKey(req.IDC, req.SN)
	serverEntry := models.ServerEntry{
		SN:      req.SN,
		Status:  "pending",
		MAC:     req.MAC,
		AddedAt: time.Now(),
	}

	if err := cp.etcdClient.Put(serverKey, serverEntry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to add server: %v", err)})
		return
	}

	// Step 2: Initialize task (MERGED STRUCTURE)
	taskID := fmt.Sprintf("task-%s", uuid.New().String()[:8])
	taskKey := etcd.TaskKeyV3(req.IDC, req.SN)

	task := models.TaskV3{
		TaskID:      taskID,
		SN:          req.SN,
		MAC:         req.MAC,
		OSType:      req.OSType,
		OSVersion:   req.OSVersion,
		DiskLayout:  req.DiskLayout,
		NetworkConf: req.NetworkConf,
		Status:      models.TaskStatusPending,
		StatusHistory: []models.StatusChange{
			{
				Status:    models.TaskStatusPending,
				Timestamp: time.Now(),
				Reason:    "Task created",
			},
		},
		Progress:  []models.ProgressStep{},
		Logs:      []string{fmt.Sprintf("[INFO] Task created for %s in %s", req.SN, req.IDC)},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		CreatedBy: "admin",
	}

	if err := cp.etcdClient.Put(taskKey, task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create task: %v", err)})
		return
	}

	log.Printf("[%s] Created task %s for server %s", req.IDC, taskID, req.SN)

	// Broadcast via WebSocket
	cp.wsHub.BroadcastStatus(taskID, task.Status)

	c.JSON(http.StatusCreated, task)
}

// listTasks lists all tasks across all IDCs
func (cp *ControlPlane) listTasks(c *gin.Context) {
	idc := c.Query("idc")

	var tasks []models.TaskV3
	var prefix string

	if idc != "" {
		prefix = etcd.MachinePrefix(idc)
	} else {
		prefix = "/os/"
	}

	kvs, err := cp.etcdClient.GetWithPrefix(prefix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for key, value := range kvs {
		if strings.HasSuffix(key, "/task") {
			var task models.TaskV3
			if err := json.Unmarshal(value, &task); err == nil {
				tasks = append(tasks, task)
			}
		}
	}

	c.JSON(http.StatusOK, tasks)
}

// getTask retrieves a specific task
func (cp *ControlPlane) getTask(c *gin.Context) {
	idc := c.Param("idc")
	sn := c.Param("sn")

	taskKey := etcd.TaskKeyV3(idc, sn)
	var task models.TaskV3

	if err := cp.etcdClient.GetJSON(taskKey, &task); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	c.JSON(http.StatusOK, task)
}

// approveTask approves a task using ATOMIC UPDATE
func (cp *ControlPlane) approveTask(c *gin.Context) {
	idc := c.Param("idc")
	sn := c.Param("sn")

	var req models.ApprovalRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	taskKey := etcd.TaskKeyV3(idc, sn)

	// Atomic update
	err := cp.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var task models.TaskV3
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}

		// Update approval
		now := time.Now()
		task.Approval = &models.Approval{
			Status:     models.ApprovalStatusApproved,
			ApprovedBy: "admin",
			ApprovedAt: &now,
			Notes:      req.Notes,
		}

		// Update status
		task.Status = models.TaskStatusApproved
		task.StatusHistory = append(task.StatusHistory, models.StatusChange{
			Status:    models.TaskStatusApproved,
			Timestamp: now,
			Reason:    "Approved by admin",
		})

		task.Logs = append(task.Logs, fmt.Sprintf("[INFO] Task approved: %s", req.Notes))
		task.UpdatedAt = now

		return task, nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] Approved task for %s", idc, sn)

	// Broadcast update
	var task models.TaskV3
	cp.etcdClient.GetJSON(taskKey, &task)
	cp.wsHub.BroadcastStatus(task.TaskID, task.Status)

	c.JSON(http.StatusOK, gin.H{"message": "Task approved"})
}

// rejectTask rejects a task using ATOMIC UPDATE
func (cp *ControlPlane) rejectTask(c *gin.Context) {
	idc := c.Param("idc")
	sn := c.Param("sn")

	var req models.ApprovalRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	taskKey := etcd.TaskKeyV3(idc, sn)

	// Atomic update
	err := cp.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
		var task models.TaskV3
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, err
		}

		// Update approval
		now := time.Now()
		task.Approval = &models.Approval{
			Status:     models.ApprovalStatusRejected,
			RejectedBy: "admin",
			RejectedAt: &now,
			Reason:     req.Reason,
		}

		// Update status
		task.Status = models.TaskStatusFailed
		task.StatusHistory = append(task.StatusHistory, models.StatusChange{
			Status:    models.TaskStatusFailed,
			Timestamp: now,
			Reason:    fmt.Sprintf("Rejected: %s", req.Reason),
		})

		task.Logs = append(task.Logs, fmt.Sprintf("[ERROR] Task rejected: %s", req.Reason))
		task.UpdatedAt = now

		return task, nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[%s] Rejected task for %s: %s", idc, sn, req.Reason)
	c.JSON(http.StatusOK, gin.H{"message": "Task rejected"})
}

// listServers lists all servers in an IDC (INDIVIDUAL KEYS)
func (cp *ControlPlane) listServers(c *gin.Context) {
	idc := c.Param("idc")
	prefix := etcd.ServerPrefix(idc)

	kvs, err := cp.etcdClient.GetWithPrefix(prefix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var servers []models.ServerEntry
	for _, value := range kvs {
		var server models.ServerEntry
		if err := json.Unmarshal(value, &server); err == nil {
			servers = append(servers, server)
		}
	}

	c.JSON(http.StatusOK, servers)
}

// getStats retrieves statistics for an IDC
func (cp *ControlPlane) getStats(c *gin.Context) {
	idc := c.Param("idc")
	statsKey := etcd.StatsKey(idc)

	var stats models.IDCStats
	if err := cp.etcdClient.GetJSON(statsKey, &stats); err != nil {
		// Calculate stats if not cached
		stats = cp.calculateStats(idc)
		cp.etcdClient.Put(statsKey, stats)
	}

	c.JSON(http.StatusOK, stats)
}

// getAllStats retrieves statistics for all IDCs
func (cp *ControlPlane) getAllStats(c *gin.Context) {
	prefix := etcd.KeyPrefixGlobalStats
	kvs, err := cp.etcdClient.GetWithPrefix(prefix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var allStats []models.IDCStats
	for _, value := range kvs {
		var stats models.IDCStats
		if err := json.Unmarshal(value, &stats); err == nil {
			allStats = append(allStats, stats)
		}
	}

	c.JSON(http.StatusOK, allStats)
}

// calculateStats calculates statistics for an IDC
func (cp *ControlPlane) calculateStats(idc string) models.IDCStats {
	stats := models.IDCStats{
		IDC:         idc,
		LastUpdated: time.Now(),
	}

	prefix := etcd.MachinePrefix(idc)
	kvs, _ := cp.etcdClient.GetWithPrefix(prefix)

	for key, value := range kvs {
		if strings.HasSuffix(key, "/task") {
			var task models.TaskV3
			if err := json.Unmarshal(value, &task); err == nil {
				stats.TotalMachines++
				switch task.Status {
				case models.TaskStatusPending:
					stats.Pending++
				case models.TaskStatusInstalling:
					stats.Installing++
				case models.TaskStatusCompleted:
					stats.Completed++
				case models.TaskStatusFailed:
					stats.Failed++
				}
			}
		}
	}

	return stats
}

// watchTasks watches for task updates and broadcasts via WebSocket
func (cp *ControlPlane) watchTasks() {
	watchChan := cp.etcdClient.Watch(cp.ctx, "/os/", true)

	for watchResp := range watchChan {
		for _, event := range watchResp.Events {
			key := string(event.Kv.Key)

			// Only process task updates
			if !strings.HasSuffix(key, "/task") {
				continue
			}

			if event.Type == clientv3.EventTypePut {
				var task models.TaskV3
				if err := json.Unmarshal(event.Kv.Value, &task); err == nil {
					// Extract IDC from key
					parts := strings.Split(key, "/")
					if len(parts) >= 3 {
						cp.wsHub.BroadcastStatus(task.TaskID, task.Status)
					}
				}
			}
		}
	}
}

// watchLeases watches for lease deletions (agent offline detection)
func (cp *ControlPlane) watchLeases() {
	watchChan := cp.etcdClient.Watch(cp.ctx, "/os/", true)

	for watchResp := range watchChan {
		for _, event := range watchResp.Events {
			key := string(event.Kv.Key)

			// Detect lease deletions
			if event.Type == clientv3.EventTypeDelete && strings.HasSuffix(key, "/lease") {
				parts := strings.Split(key, "/")
				if len(parts) >= 5 {
					idc := parts[2]
					sn := parts[4]

					log.Printf("[%s] Agent offline detected: %s", idc, sn)

					// Mark task as failed using atomic update
					taskKey := etcd.TaskKeyV3(idc, sn)
					cp.etcdClient.AtomicUpdate(taskKey, func(data []byte) (interface{}, error) {
						var task models.TaskV3
						json.Unmarshal(data, &task)

						if task.Status == models.TaskStatusInstalling {
							task.Status = models.TaskStatusFailed
							task.StatusHistory = append(task.StatusHistory, models.StatusChange{
								Status:    models.TaskStatusFailed,
								Timestamp: time.Now(),
								Reason:    "Agent went offline (lease expired)",
							})
							task.Logs = append(task.Logs, "[ERROR] Agent connection lost")
							task.UpdatedAt = time.Now()
						}

						return task, nil
					})
				}
			}
		}
	}
}
