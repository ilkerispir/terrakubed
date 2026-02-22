package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/executor/terraform"
	"github.com/ilkerispir/terrakubed/internal/model"
)

func (p *JobProcessor) uploadStateAndOutput(job *model.TerraformJob, workingDir string) {
	// Upload terraform.tfstate if exists
	statePath := filepath.Join(workingDir, "terraform.tfstate")
	if _, err := os.Stat(statePath); err == nil {
		f, err := os.Open(statePath)
		if err == nil {
			defer f.Close()
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
			remotePath := fmt.Sprintf("organization/%s/workspace/%s/job/%s/step/%s/terraformLibrary.tfplan", job.OrganizationId, job.WorkspaceId, job.JobId, job.StepId)
			if err := p.Storage.UploadFile(remotePath, f); err != nil {
				log.Printf("Failed to upload plan: %v", err)
			}
		}
	}

	// For Apply/Destroy: save state JSON, raw state, output, and create history
	if job.Type == "terraformApply" || job.Type == "terraformDestroy" {
		execPath, err := p.VersionManager.Install(job.TerraformVersion, job.Tofu)
		if err != nil {
			log.Printf("Failed to install terraform for state operations: %v", err)
			return
		}

		tfExecutor := terraform.NewExecutor(job, workingDir, nil, execPath)

		// Save state JSON (terraform show)
		stateJson, err := tfExecutor.ShowState()
		if err != nil {
			log.Printf("Failed to get state JSON: %v", err)
		} else {
			stateJsonPath := fmt.Sprintf("tfstate/%s/%s/state/state.json", job.OrganizationId, job.WorkspaceId)
			if err := p.Storage.UploadFile(stateJsonPath, strings.NewReader(stateJson)); err != nil {
				log.Printf("Failed to upload state JSON: %v", err)
			}
		}

		// Save raw state (terraform state pull)
		rawState, err := tfExecutor.StatePull()
		if err != nil {
			log.Printf("Failed to pull raw state: %v", err)
		} else {
			rawStatePath := fmt.Sprintf("tfstate/%s/%s/state/state.raw.json", job.OrganizationId, job.WorkspaceId)
			if err := p.Storage.UploadFile(rawStatePath, strings.NewReader(rawState)); err != nil {
				log.Printf("Failed to upload raw state: %v", err)
			}
		}

		// Get and save terraform output
		outputJson, err := tfExecutor.Output()
		if err != nil {
			log.Printf("Failed to get terraform output: %v", err)
		} else {
			job.TerraformOutput = outputJson
		}

		// Upload step output
		outputPath := fmt.Sprintf("tfoutput/%s/%s/%s.tfoutput", job.OrganizationId, job.JobId, job.StepId)
		if job.TerraformOutput != "" {
			if err := p.Storage.UploadFile(outputPath, strings.NewReader(job.TerraformOutput)); err != nil {
				log.Printf("Failed to upload terraform output: %v", err)
			}
		}

		// Create history record
		stateURL := fmt.Sprintf("tfstate/%s/%s/state/state.json", job.OrganizationId, job.WorkspaceId)
		if err := p.Status.CreateHistory(job, stateURL); err != nil {
			log.Printf("Failed to create history record: %v", err)
		}
	}
}
