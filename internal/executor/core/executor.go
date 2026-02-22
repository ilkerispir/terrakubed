package core

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/auth"
	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/executor/logs"
	"github.com/ilkerispir/terrakubed/internal/executor/script"
	"github.com/ilkerispir/terrakubed/internal/executor/terraform"
	"github.com/ilkerispir/terrakubed/internal/executor/workspace"
	"github.com/ilkerispir/terrakubed/internal/model"
	"github.com/ilkerispir/terrakubed/internal/status"
	"github.com/ilkerispir/terrakubed/internal/storage"
)

type JobProcessor struct {
	Status         status.StatusService
	Config         *config.Config
	Storage        storage.StorageService
	VersionManager *terraform.VersionManager
}

func NewJobProcessor(cfg *config.Config, status status.StatusService, storage storage.StorageService) *JobProcessor {
	return &JobProcessor{
		Config:         cfg,
		Status:         status,
		Storage:        storage,
		VersionManager: terraform.NewVersionManager(),
	}
}

func stripScheme(domain string) string {
	u, err := url.Parse(domain)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	return domain
}

func (p *JobProcessor) generateTerraformCredentials(job *model.TerraformJob, workingDir string) error {
	var token string
	log.Printf("generateTerraformCredentials: checking InternalSecret (len: %d)", len(p.Config.InternalSecret))
	if p.Config.InternalSecret != "" {
		t, err := auth.GenerateTerrakubeToken(p.Config.InternalSecret)
		if err != nil {
			log.Printf("Warning: failed to generate Terrakube token for .terraformrc: %v", err)
		} else {
			token = t
			log.Printf("generateTerraformCredentials: token generated successfully")
		}
	} else {
		log.Printf("Warning: InternalSecret is empty, skipping token generation")
	}

	if token == "" {
		return nil
	}

	content := ""

	registryHost := stripScheme(p.Config.TerrakubeRegistryDomain)
	if registryHost != "" {
		content += fmt.Sprintf("credentials \"%s\" {\n  token = \"%s\"\n}\n", registryHost, token)
		log.Printf("generateTerraformCredentials: added credentials for registryHost: %s", registryHost)
	}

	if p.Config.AzBuilderApiUrl != "" {
		parsedUrl, err := url.Parse(p.Config.AzBuilderApiUrl)
		if err == nil && parsedUrl.Hostname() != "" {
			apiHost := parsedUrl.Hostname()
			if apiHost != registryHost {
				content += fmt.Sprintf("credentials \"%s\" {\n  token = \"%s\"\n}\n", apiHost, token)
				log.Printf("generateTerraformCredentials: added credentials for apiHost: %s", apiHost)
			}
		}
	}

	if content == "" {
		log.Printf("generateTerraformCredentials: no credentials generated, returning")
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	rcPath := filepath.Join(homeDir, ".terraformrc")
	log.Printf("generateTerraformCredentials: writing HCL credentials to %s", rcPath)
	return os.WriteFile(rcPath, []byte(content), 0644)
}

func (p *JobProcessor) generateBackendOverride(job *model.TerraformJob, workingDir string) error {
	statePath := filepath.Join(workingDir, "terraform.tfstate")
	overrideContent := fmt.Sprintf(`terraform {
  backend "local" {
    path = "%s"
  }
}
`, statePath)

	overridePath := filepath.Join(workingDir, "terrakube_override.tf")
	log.Printf("generateBackendOverride: using local backend with state at %s", statePath)
	return os.WriteFile(overridePath, []byte(overrideContent), 0644)
}

func readCommitHash(workingDir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (p *JobProcessor) ProcessJob(job *model.TerraformJob) error {
	// Ensure nil maps/slices from JSON deserialization are initialized
	// The Java API may send null for these fields when they are not set
	if job.EnvironmentVariables == nil {
		job.EnvironmentVariables = make(map[string]string)
	}
	if job.Variables == nil {
		job.Variables = make(map[string]string)
	}
	if job.CommandList == nil {
		job.CommandList = []model.Command{}
	}

	log.Printf("Processing Job: %s", job.JobId)

	// 1. Update Status to Running
	if err := p.Status.SetRunning(job); err != nil {
		log.Printf("Failed to set running status: %v", err)
	}

	// 2. Setup Logging
	var baseStreamer logs.LogStreamer
	if os.Getenv("USE_REDIS_LOGS") == "true" {
		baseStreamer = logs.NewRedisStreamer(os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PASSWORD"), job.JobId, job.StepId)
	} else {
		baseStreamer = &logs.ConsoleStreamer{}
	}

	var logBuffer bytes.Buffer
	streamer := logs.NewMultiStreamer(baseStreamer, &logBuffer)
	defer streamer.Close()

	// 3. Setup Workspace
	ws := workspace.NewWorkspace(job)
	workingDir, err := ws.Setup()
	if err != nil {
		p.Status.SetCompleted(job, false, err.Error())
		return fmt.Errorf("failed to setup workspace: %w", err)
	}
	defer ws.Cleanup()

	// 3b. Capture and update commit ID
	commitId := readCommitHash(workingDir)
	if commitId != "" {
		if err := p.Status.UpdateCommitId(job, commitId); err != nil {
			log.Printf("Failed to update commit ID: %v", err)
		}
	}

	// 4. Download Pre-existing State
	remotePath := fmt.Sprintf("organization/%s/workspace/%s/state/terraform.tfstate", job.OrganizationId, job.WorkspaceId)
	stateReader, err := p.Storage.DownloadFile(remotePath)
	if err != nil {
		log.Printf("No existing state found (this is normal for new workspaces): %v", err)
	} else if stateReader != nil {
		defer stateReader.Close()
		localStatePath := filepath.Join(workingDir, "terraform.tfstate")
		f, err := os.Create(localStatePath)
		if err != nil {
			log.Printf("Failed to create local state file: %v", err)
		} else {
			if _, err := io.Copy(f, stateReader); err != nil {
				log.Printf("Failed to write state file: %v", err)
			}
			f.Close()
			log.Printf("Downloaded existing state to %s", localStatePath)
		}
	}

	// 4b. Download Plan for Apply
	if job.Type == "terraformApply" {
		p.downloadPlanForApply(job, workingDir)
	}

	// 5. Execute Command
	var executionErr error
	switch job.Type {
	case "terraformPlan", "terraformApply", "terraformDestroy":
		executionErr = p.executeTerraform(job, workingDir, streamer, &logBuffer)

	case "customScripts", "approval":
		scriptExecutor := script.NewExecutor(job, workingDir, streamer)
		executionErr = scriptExecutor.Execute()

		output := logBuffer.String()
		if executionErr != nil {
			output += "\nError: " + executionErr.Error()
			p.Status.SetCompleted(job, false, output)
		} else {
			p.Status.SetCompleted(job, true, output)
		}
	default:
		executionErr = fmt.Errorf("unknown job type: %s", job.Type)
		p.Status.SetCompleted(job, false, executionErr.Error())
	}

	return executionErr
}

func (p *JobProcessor) executeTerraform(job *model.TerraformJob, workingDir string, streamer logs.LogStreamer, logBuffer *bytes.Buffer) error {
	execPath, err := p.VersionManager.Install(job.TerraformVersion, job.Tofu)
	if err != nil {
		return fmt.Errorf("failed to install terraform %s: %w", job.TerraformVersion, err)
	}

	if err := p.generateBackendOverride(job, workingDir); err != nil {
		return fmt.Errorf("failed to generate backend override: %w", err)
	}

	if err := p.generateTerraformCredentials(job, workingDir); err != nil {
		return fmt.Errorf("failed to generate terraform credentials: %w", err)
	}

	// Execute beforeInit scripts
	scriptExec := script.NewExecutor(job, workingDir, streamer)
	if err := scriptExec.ExecutePhase("beforeInit"); err != nil {
		return fmt.Errorf("beforeInit scripts failed: %w", err)
	}

	tfExecutor := terraform.NewExecutor(job, workingDir, streamer, execPath)
	result, err := tfExecutor.Execute()

	if err != nil {
		scriptExec.ExecutePhase("onFailure")

		output := logBuffer.String() + "\nError: " + err.Error()
		p.Status.SetCompleted(job, false, output)
		return err
	}

	// Execute after scripts
	if err := scriptExec.ExecutePhase("after"); err != nil {
		log.Printf("Warning: after scripts failed: %v", err)
	}

	// Upload State and Output
	p.uploadStateAndOutput(job, workingDir)

	// Set final status based on result
	output := logBuffer.String()
	if job.Type == "terraformPlan" && result != nil && result.ExitCode == 2 {
		if err := p.Status.SetPending(job, output); err != nil {
			log.Printf("Failed to set pending status: %v", err)
		}
	} else {
		if err := p.Status.SetCompleted(job, true, output); err != nil {
			log.Printf("Failed to set completed status: %v", err)
		}
	}

	return nil
}

func (p *JobProcessor) downloadPlanForApply(job *model.TerraformJob, workingDir string) {
	remotePath := fmt.Sprintf("organization/%s/workspace/%s/job/%s/step/%s/terraformLibrary.tfplan",
		job.OrganizationId, job.WorkspaceId, job.JobId, job.StepId)

	reader, err := p.Storage.DownloadFile(remotePath)
	if err != nil {
		log.Printf("No saved plan found for apply (will run fresh apply): %v", err)
		return
	}
	defer reader.Close()

	localPlanPath := filepath.Join(workingDir, "terraformLibrary.tfPlan")
	f, err := os.Create(localPlanPath)
	if err != nil {
		log.Printf("Failed to create local plan file: %v", err)
		return
	}

	if _, err := io.Copy(f, reader); err != nil {
		log.Printf("Failed to write plan file: %v", err)
	}
	f.Close()
	log.Printf("Downloaded saved plan to %s", localPlanPath)
}
