package core

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/auth"
	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/executor/logs"
	"github.com/ilkerispir/terrakubed/internal/model"
	"github.com/ilkerispir/terrakubed/internal/executor/script"
	"github.com/ilkerispir/terrakubed/internal/status"
	"github.com/ilkerispir/terrakubed/internal/storage"
	"github.com/ilkerispir/terrakubed/internal/executor/terraform"
	"github.com/ilkerispir/terrakubed/internal/executor/workspace"
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
	// Fallback to manual strip if no scheme provided
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

	if p.Config.TerrakubeApiUrl != "" {
		parsedUrl, err := url.Parse(p.Config.TerrakubeApiUrl)
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
	log.Printf("generateBackendOverride checking API URL: TerrakubeApiUrl=%s", p.Config.TerrakubeApiUrl)
	if p.Config.TerrakubeApiUrl == "" {
		return nil
	}

	parsedUrl, err := url.Parse(p.Config.TerrakubeApiUrl)
	if err != nil {
		return fmt.Errorf("invalid TerrakubeApiUrl: %v", err)
	}
	hostname := parsedUrl.Hostname()

	orgName := job.EnvironmentVariables["organizationName"]
	if orgName == "" {
		orgName = job.OrganizationId
	}

	workspaceName := job.EnvironmentVariables["workspaceName"]
	if workspaceName == "" {
		workspaceName = job.WorkspaceId
	}

	overrideContent := fmt.Sprintf(`terraform {
  backend "remote" {
    hostname     = "%s"
    organization = "%s"
    workspaces {
      name = "%s"
    }
  }
}
`, hostname, orgName, workspaceName)

	overridePath := filepath.Join(workingDir, "terrakube_override.tf")
	return os.WriteFile(overridePath, []byte(overrideContent), 0644)
}

func (p *JobProcessor) ProcessJob(job *model.TerraformJob) error {
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

	// 4. Download Pre-existing State/Plan if needed
	// TODO: If APPLY, download PLAN
	// TODO: If PLAN/APPLY/DESTROY, download STATE (if not using remote backend)

	// 5. Execute Command
	var executionErr error
	switch job.Type {
	case "terraformPlan", "terraformApply", "terraformDestroy":
		// Install/Get execution path for the specific version
		execPath, err := p.VersionManager.Install(job.TerraformVersion)
		if err != nil {
			executionErr = fmt.Errorf("failed to install terraform %s: %w", job.TerraformVersion, err)
			break
		}

		if err := p.generateBackendOverride(job, workingDir); err != nil {
			executionErr = fmt.Errorf("failed to generate backend override: %w", err)
			break
		}

		if err := p.generateTerraformCredentials(job, workingDir); err != nil {
			executionErr = fmt.Errorf("failed to generate terraform credentials: %w", err)
			break
		}

		tfExecutor := terraform.NewExecutor(job, workingDir, streamer, execPath)
		executionErr = tfExecutor.Execute()

		// Upload State and Output
		if executionErr == nil {
			p.uploadStateAndOutput(job, workingDir)
		}

	case "customScripts", "approval":
		scriptExecutor := script.NewExecutor(job, workingDir, streamer)
		executionErr = scriptExecutor.Execute()
	default:
		executionErr = fmt.Errorf("unknown job type: %s", job.Type)
	}

	// 6. Update Status to Completed/Failed
	success := executionErr == nil
	output := logBuffer.String()
	if executionErr != nil {
		output += "\nError: " + executionErr.Error()
	}

	if err := p.Status.SetCompleted(job, success, output); err != nil {
		log.Printf("Failed to set completed status: %v", err)
	}

	return executionErr
}
