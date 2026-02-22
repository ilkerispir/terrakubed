package batch

import (
	"log"

	"github.com/ilkerispir/terrakubed/internal/executor/core"
	"github.com/ilkerispir/terrakubed/internal/model"
)

func AdjustAndExecute(job *model.TerraformJob, processor *core.JobProcessor) {
	log.Printf("Starting Batch Execution for Job %s", job.JobId)
	if err := processor.ProcessJob(job); err != nil {
		// Log but don't Fatalf - ProcessJob already reported failure to the API.
		// Exiting non-zero would cause K8s Job to retry the pod unnecessarily.
		log.Printf("Job execution failed: %v", err)
	}
	log.Println("Batch execution finished")
}
