package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ilkerispir/terrakubed/internal/model"
	"github.com/ilkerispir/terrakubed/internal/executor/terraform"
)

func (p *JobProcessor) uploadStateAndOutput(job *model.TerraformJob, workingDir string) {
	// Paths based on typical Terrakube Storage structure (need verification of exact paths)
	// Plan: organization/%s/workspace/%s/job/%s/step/%s/terraformLibrary.tfplan
	// State: organization/%s/workspace/%s/state/terraform.tfstate

	// Upload terraform.tfstate if exists
	statePath := filepath.Join(workingDir, "terraform.tfstate")
	if _, err := os.Stat(statePath); err == nil {
		f, err := os.Open(statePath)
		if err == nil {
			defer f.Close()
			// Path: organization/{orgId}/workspace/{workspaceId}/state/terraform.tfstate
			remotePath := fmt.Sprintf("organization/%s/workspace/%s/state/terraform.tfstate", job.OrganizationId, job.WorkspaceId)
			if err := p.Storage.UploadFile(remotePath, f); err != nil {
				log.Printf("Failed to upload state: %v", err)
			}
		}
	}

	// Upload Plan if exists (terraformPlan)
	planPath := filepath.Join(workingDir, "terraform.tfplan")
	if _, err := os.Stat(planPath); err == nil {
		f, err := os.Open(planPath)
		if err == nil {
			defer f.Close()
			// Path: organization/{orgId}/workspace/{workspaceId}/job/{jobId}/step/{stepId}/terraformLibrary.tfplan
			remotePath := fmt.Sprintf("organization/%s/workspace/%s/job/%s/step/%s/terraformLibrary.tfplan", job.OrganizationId, job.WorkspaceId, job.JobId, job.StepId)
			if err := p.Storage.UploadFile(remotePath, f); err != nil {
				log.Printf("Failed to upload plan: %v", err)
			}
		}
	}

	// Generate and Upload Output JSON (only for Apply)
	if job.Type == "terraformApply" {
		// Re-instantiate executor just for Output
		execPath, err := p.VersionManager.Install(job.TerraformVersion)
		if err == nil {
			tfExecutor := terraform.NewExecutor(job, workingDir, nil, execPath)
			outputJson, err := tfExecutor.Output()
			if err == nil {
				job.TerraformOutput = outputJson
			} else {
				log.Printf("Failed to get terraform output: %v", err)
			}
		}
	}
}
