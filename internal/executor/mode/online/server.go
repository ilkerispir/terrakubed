package online

import (
	"encoding/json"
	"log"
	"net/http"

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

		// Process job asynchronously
		// In a real implementation this should be offloaded to a worker pool
		go func() {
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
