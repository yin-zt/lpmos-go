package api

import (
	"github.com/gin-gonic/gin"
	"github.com/lpmos/lpmos-go/pkg/etcd"
)

// SetupRouter creates and configures the Gin router
func SetupRouter(etcdClient *etcd.Client) *gin.Engine {
	router := gin.Default()

	handler := NewHandler(etcdClient)

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
				"service": "control-plane",
			})
		})
	}

	return router
}
