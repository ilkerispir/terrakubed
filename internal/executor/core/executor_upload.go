package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/ilkerispir/terrakubed/internal/executor/terraform"
	"github.com/ilkerispir/terrakubed/internal/model"
)

func (p *JobProcessor) uploadStateAndOutput(job *model.TerraformJob, workingDir string) {
	// Upload Plan if exists (terraformPlan / terraformPlanDestroy).
	// Stored at a job-level path (no step ID) so the apply step can always
	// find it regardless of its own step ID.
	planPath := filepath.Join(workingDir, "terraform.tfplan")
	if _, err := os.Stat(planPath); err == nil {
		f, err := os.Open(planPath)
		if err == nil {
			defer f.Close()
			remotePath := fmt.Sprintf("organization/%s/workspace/%s/job/%s/plan/terraformLibrary.tfplan", job.OrganizationId, job.WorkspaceId, job.JobId)
			if err := p.Storage.UploadFile(remotePath, f); err != nil {
				log.Printf("Failed to upload plan: %v", err)
			}
		}
	}

	// For Apply/Destroy: save state JSON, raw state, output, and create history.
	// NOTE: terraform.tfstate is NOT uploaded here — the S3/Azure/GCS backend
	// configured via terrakube_override.tf writes state directly to cloud storage.
	if job.Type == "terraformApply" || job.Type == "terraformDestroy" {
		execPath, err := p.VersionManager.Install(job.TerraformVersion, job.Tofu)
		if err != nil {
			log.Printf("Failed to install terraform for state operations: %v", err)
			return
		}

		tfExecutor := terraform.NewExecutor(job, workingDir, nil, execPath)

		// Save state JSON (terraform show) — UUID filename matches Java API history protocol.
		// Each apply creates a new immutable snapshot; the history record links to it.
		stateFilename := uuid.New().String()
		stateJson, err := tfExecutor.ShowState()
		if err != nil {
			log.Printf("Failed to get state JSON: %v", err)
		} else {
			stateJsonPath := fmt.Sprintf("tfstate/%s/%s/state/%s.json", job.OrganizationId, job.WorkspaceId, stateFilename)
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

		// Create history record — URL matches Java API TerraformStatePathService format:
		// {api_url}/tfstate/v1/organization/{orgId}/workspace/{wsId}/state/{UUID}.json
		stateURL := fmt.Sprintf("%s/tfstate/v1/organization/%s/workspace/%s/state/%s.json",
			p.Config.AzBuilderApiUrl, job.OrganizationId, job.WorkspaceId, stateFilename)
		if err := p.Status.CreateHistory(job, stateURL); err != nil {
			log.Printf("Failed to create history record: %v", err)
		}
	}
}
