package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lpmos/lpmos-go/pkg/etcd"
	"github.com/lpmos/lpmos-go/pkg/models"
)

// Handler contains API handler methods
type Handler struct {
	etcdClient *etcd.Client
}

// NewHandler creates a new API handler
func NewHandler(etcdClient *etcd.Client) *Handler {
	return &Handler{
		etcdClient: etcdClient,
	}
}

// CreateTask handles POST /api/v1/tasks
func (h *Handler) CreateTask(c *gin.Context) {
	var req models.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create task
	task := &models.Task{
		ID:          uuid.New().String(),
		RegionID:    req.RegionID,
		TargetMAC:   strings.ToLower(req.TargetMAC),
		OSType:      req.OSType,
		OSVersion:   req.OSVersion,
		DiskLayout:  req.DiskLayout,
		NetworkConf: req.NetworkConf,
		CreatedAt:   time.Now(),
		CreatedBy:   "admin@example.com", // TODO: Get from auth context
		Tags:        req.Tags,
		Status:      models.TaskStatusPending,
		UpdatedAt:   time.Now(),
	}

	// Store in etcd
	if err := h.storeTask(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to store task: %v", err)})
		return
	}

	// Assign task to region
	if err := h.assignTaskToRegion(task.ID, task.RegionID); err != nil {
		log.Printf("Warning: failed to assign task to region: %v", err)
	}

	c.JSON(http.StatusCreated, gin.H{
		"task_id":    task.ID,
		"status":     task.Status,
		"created_at": task.CreatedAt,
		"links": gin.H{
			"self":    fmt.Sprintf("/api/v1/tasks/%s", task.ID),
			"approve": fmt.Sprintf("/api/v1/tasks/%s/approve", task.ID),
		},
	})
}

// GetTask handles GET /api/v1/tasks/:id
func (h *Handler) GetTask(c *gin.Context) {
	taskID := c.Param("id")

	task, err := h.loadTask(taskID)
	if err != nil {
		if etcd.IsKeyNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load task: %v", err)})
		return
	}

	c.JSON(http.StatusOK, task)
}

// ListTasks handles GET /api/v1/tasks
func (h *Handler) ListTasks(c *gin.Context) {
	// Get query parameters
	regionID := c.Query("region_id")
	status := c.Query("status")

	// Get all tasks
	kvs, err := h.etcdClient.GetWithPrefix(etcd.KeyPrefixTasks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list tasks: %v", err)})
		return
	}

	var tasks []*models.Task
	for key, value := range kvs {
		// Only process metadata keys
		if !strings.HasSuffix(key, "/metadata") {
			continue
		}

		var task models.Task
		if err := json.Unmarshal(value, &task); err != nil {
			log.Printf("Warning: failed to unmarshal task: %v", err)
			continue
		}

		// Load status
		taskID := strings.TrimPrefix(key, etcd.KeyPrefixTasks)
		taskID = strings.TrimSuffix(taskID, "/metadata")
		if statusBytes, err := h.etcdClient.Get(etcd.TaskKey(taskID, "status")); err == nil {
			task.Status = models.TaskStatus(statusBytes)
		}

		// Filter by region
		if regionID != "" && task.RegionID != regionID {
			continue
		}

		// Filter by status
		if status != "" && string(task.Status) != status {
			continue
		}

		tasks = append(tasks, &task)
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
		"total": len(tasks),
	})
}

// ApproveTask handles PUT /api/v1/tasks/:id/approve
func (h *Handler) ApproveTask(c *gin.Context) {
	taskID := c.Param("id")

	var req models.ApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Load task
	task, err := h.loadTask(taskID)
	if err != nil {
		if etcd.IsKeyNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load task: %v", err)})
		return
	}

	// Check if task is in pending_approval status
	if task.Status != models.TaskStatusPendingApproval {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("task is not in pending_approval status (current: %s)", task.Status)})
		return
	}

	// Create approval
	now := time.Now()
	approval := &models.Approval{
		Notes: req.Notes,
	}

	if req.Approved {
		approval.Status = models.ApprovalStatusApproved
		approval.ApprovedBy = "admin@example.com" // TODO: Get from auth context
		approval.ApprovedAt = &now
		task.Status = models.TaskStatusApproved
	} else {
		approval.Status = models.ApprovalStatusRejected
		approval.RejectedBy = "admin@example.com" // TODO: Get from auth context
		approval.RejectedAt = &now
		approval.Reason = req.Reason
		task.Status = models.TaskStatusFailed
	}

	// Update in etcd
	if err := h.etcdClient.Put(etcd.TaskKey(taskID, "approval"), approval); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to store approval: %v", err)})
		return
	}

	if err := h.etcdClient.Put(etcd.TaskKey(taskID, "status"), string(task.Status)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update status: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id":     taskID,
		"approval":    approval,
		"next_status": task.Status,
	})
}

// DeleteTask handles DELETE /api/v1/tasks/:id
func (h *Handler) DeleteTask(c *gin.Context) {
	taskID := c.Param("id")

	// Delete all keys related to the task
	keys := []string{
		etcd.TaskKey(taskID, "metadata"),
		etcd.TaskKey(taskID, "status"),
		etcd.TaskKey(taskID, "hardware"),
		etcd.TaskKey(taskID, "approval"),
		etcd.TaskKey(taskID, "error"),
	}

	for _, key := range keys {
		if err := h.etcdClient.Delete(key); err != nil {
			log.Printf("Warning: failed to delete key %s: %v", key, err)
		}
	}

	c.Status(http.StatusNoContent)
}

// Helper methods

func (h *Handler) storeTask(task *models.Task) error {
	// Store metadata
	if err := h.etcdClient.Put(etcd.TaskKey(task.ID, "metadata"), task); err != nil {
		return err
	}

	// Store status
	if err := h.etcdClient.Put(etcd.TaskKey(task.ID, "status"), string(task.Status)); err != nil {
		return err
	}

	return nil
}

func (h *Handler) loadTask(taskID string) (*models.Task, error) {
	var task models.Task

	// Load metadata
	if err := h.etcdClient.GetJSON(etcd.TaskKey(taskID, "metadata"), &task); err != nil {
		return nil, err
	}

	// Load status
	if statusBytes, err := h.etcdClient.Get(etcd.TaskKey(taskID, "status")); err == nil {
		task.Status = models.TaskStatus(statusBytes)
	}

	// Load hardware if exists
	var hardware models.HardwareInfo
	if err := h.etcdClient.GetJSON(etcd.TaskKey(taskID, "hardware"), &hardware); err == nil {
		task.Hardware = &hardware
	}

	// Load approval if exists
	var approval models.Approval
	if err := h.etcdClient.GetJSON(etcd.TaskKey(taskID, "approval"), &approval); err == nil {
		task.Approval = &approval
	}

	// Load error if exists
	var taskErr models.TaskError
	if err := h.etcdClient.GetJSON(etcd.TaskKey(taskID, "error"), &taskErr); err == nil {
		task.Error = &taskErr
	}

	return &task, nil
}

func (h *Handler) assignTaskToRegion(taskID, regionID string) error {
	// Create a task assignment key in the region
	key := etcd.RegionKey(regionID, "tasks", taskID)
	return h.etcdClient.PutWithLease(key, "", 3600) // 1 hour TTL
}

// StartTaskWatcher watches for task status changes and sends notifications
func (h *Handler) StartTaskWatcher(ctx context.Context) {
	watchChan := h.etcdClient.Watch(ctx, etcd.KeyPrefixTasks, true)

	log.Println("Control plane: started watching for task status changes")

	for {
		select {
		case <-ctx.Done():
			log.Println("Control plane: stopping task watcher")
			return
		case watchResp := <-watchChan:
			if watchResp.Err() != nil {
				log.Printf("Watch error: %v", watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				key := string(event.Kv.Key)

				// Check if it's a status change to pending_approval
				if strings.HasSuffix(key, "/status") && string(event.Kv.Value) == string(models.TaskStatusPendingApproval) {
					taskID := strings.TrimPrefix(key, etcd.KeyPrefixTasks)
					taskID = strings.TrimSuffix(taskID, "/status")

					log.Printf("Task %s requires approval - notifying operators", taskID)
					// TODO: Send webhook/email notification
				}
			}
		}
	}
}
