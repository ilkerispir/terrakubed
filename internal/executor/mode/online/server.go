package online

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/ilkerispir/terrakubed/internal/executor/core"
	"github.com/ilkerispir/terrakubed/internal/model"
)

func StartServer(port string, processor *core.JobProcessor) {
	r := gin.Default()

	r.POST("/api/v1/terraform-rs", func(c *gin.Context) {
		bodyBytes, _ := c.GetRawData()
		log.Printf("Received raw payload: %s", string(bodyBytes))

		var job model.TerraformJob
		if err := json.Unmarshal(bodyBytes, &job); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Process job asynchronously with panic recovery
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in job processing for job %s: %v\n%s",
						job.JobId, r, debug.Stack())
					// Try to update job status to failed
					errMsg := fmt.Sprintf("Internal executor error: %v", r)
					processor.Status.SetCompleted(&job, false, errMsg)
				}
			}()
			processor.ProcessJob(&job)
		}()

		c.JSON(http.StatusAccepted, job)
	})

	r.GET("/actuator/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})
	r.GET("/actuator/health/liveness", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})
	r.GET("/actuator/health/readiness", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})

	r.Run(":" + port)
}
