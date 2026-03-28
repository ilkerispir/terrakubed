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

// generateBackendOverride creates terrakube_override.tf that redirects Terraform's backend
// to the same cloud storage bucket Terrakube uses — matching the Java executor's approach.
// Terraform reads/writes state directly to cloud storage; no separate download/upload needed.
func (p *JobProcessor) generateBackendOverride(job *model.TerraformJob, workingDir string) error {
	stateKey := fmt.Sprintf("tfstate/%s/%s/terraform.tfstate", job.OrganizationId, job.WorkspaceId)
	var overrideContent string

	switch p.Config.StorageType {
	case "AWS":
		overrideContent = p.generateAwsBackend(stateKey)
	case "AZURE":
		overrideContent = p.generateAzureBackend(job.OrganizationId, job.WorkspaceId)
	case "GCP":
		overrideContent = p.generateGcpBackend(job.OrganizationId, job.WorkspaceId)
	default:
		// LOCAL or unknown: fall back to a local file in the working directory
		statePath := filepath.Join(workingDir, "terraform.tfstate")
		overrideContent = fmt.Sprintf("terraform {\n  backend \"local\" {\n    path = \"%s\"\n  }\n}\n", statePath)
		log.Printf("generateBackendOverride: using local backend at %s", statePath)
	}

	overridePath := filepath.Join(workingDir, "terrakube_override.tf")
	log.Printf("generateBackendOverride: storageType=%s stateKey=%s", p.Config.StorageType, stateKey)
	return os.WriteFile(overridePath, []byte(overrideContent), 0644)
}

func (p *JobProcessor) generateAwsBackend(stateKey string) string {
	var sb strings.Builder
	sb.WriteString("terraform {\n  backend \"s3\" {\n")
	sb.WriteString(fmt.Sprintf("    bucket = \"%s\"\n", p.Config.AwsBucketName))
	sb.WriteString(fmt.Sprintf("    region = \"%s\"\n", p.Config.AwsRegion))
	sb.WriteString(fmt.Sprintf("    key    = \"%s\"\n", stateKey))
	if p.Config.AwsAccessKey != "" {
		// Static credentials — omitted when IRSA/pod identity is used
		sb.WriteString(fmt.Sprintf("    access_key = \"%s\"\n", p.Config.AwsAccessKey))
		sb.WriteString(fmt.Sprintf("    secret_key = \"%s\"\n", p.Config.AwsSecretKey))
	}
	if p.Config.AwsEndpoint != "" {
		sb.WriteString(fmt.Sprintf("    endpoint                    = \"%s\"\n", p.Config.AwsEndpoint))
		sb.WriteString("    skip_credentials_validation = true\n")
		sb.WriteString("    skip_metadata_api_check     = true\n")
		sb.WriteString("    skip_region_validation      = true\n")
		sb.WriteString("    force_path_style            = true\n")
	}
	sb.WriteString("  }\n}\n")
	return sb.String()
}

func (p *JobProcessor) generateAzureBackend(orgId, wsId string) string {
	var sb strings.Builder
	sb.WriteString("terraform {\n  backend \"azurerm\" {\n")
	sb.WriteString(fmt.Sprintf("    storage_account_name = \"%s\"\n", p.Config.AzureStorageAccountName))
	sb.WriteString(fmt.Sprintf("    container_name       = \"%s\"\n", p.Config.AzureStorageContainerName))
	sb.WriteString(fmt.Sprintf("    key                  = \"%s/%s/terraform.tfstate\"\n", orgId, wsId))
	if p.Config.AzureStorageAccountKey != "" {
		sb.WriteString(fmt.Sprintf("    access_key = \"%s\"\n", p.Config.AzureStorageAccountKey))
	}
	sb.WriteString("  }\n}\n")
	return sb.String()
}

func (p *JobProcessor) generateGcpBackend(orgId, wsId string) string {
	var sb strings.Builder
	sb.WriteString("terraform {\n  backend \"gcs\" {\n")
	sb.WriteString(fmt.Sprintf("    bucket = \"%s\"\n", p.Config.GcpStorageBucketName))
	sb.WriteString(fmt.Sprintf("    prefix = \"tfstate/%s/%s\"\n", orgId, wsId))
	sb.WriteString("  }\n}\n")
	return sb.String()
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
	redisHost := os.Getenv("TerrakubeRedisHostname")
	redisPort := os.Getenv("TerrakubeRedisPort")
	redisPassword := os.Getenv("TerrakubeRedisPassword")
	if redisHost != "" {
		if redisPort == "" {
			redisPort = "6379"
		}
		addr := redisHost + ":" + redisPort
		rs, err := logs.NewRedisStreamer(addr, redisPassword, job.JobId, job.StepId)
		if err != nil {
			log.Printf("Warning: failed to connect to Redis at %s, falling back to console: %v", addr, err)
			baseStreamer = &logs.ConsoleStreamer{}
		} else {
			log.Printf("Redis log streaming enabled (addr=%s, jobId=%s, stepId=%s)", addr, job.JobId, job.StepId)
			baseStreamer = rs
		}
	} else {
		baseStreamer = &logs.ConsoleStreamer{}
	}

	var logBuffer bytes.Buffer
	streamer := logs.NewMultiStreamer(baseStreamer, &logBuffer)
	defer streamer.Close()

	// 3. Setup Workspace
	// For CLI-driven runs (branch == "remote-content") the workspace downloads a tar.gz
	// from the API. Generate a short-lived Terrakube token for that HTTP request.
	var apiToken string
	if p.Config.InternalSecret != "" {
		if t, err := auth.GenerateTerrakubeToken(p.Config.InternalSecret); err == nil {
			apiToken = t
		}
	}
	ws := workspace.NewWorkspace(job, apiToken)
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

	// 4. State is managed directly by the cloud storage backend (S3/Azure/GCS).
	// generateBackendOverride (called inside executeTerraform) creates a backend override
	// that points Terraform to the correct bucket+key, so terraform init/plan/apply
	// read and write state without any manual download or upload step.

	// 4b. Download saved plan for apply step (plan file is NOT managed by the backend)
	if job.Type == "terraformApply" {
		p.downloadPlanForApply(job, workingDir)
	}

	// 5. Execute Command
	var executionErr error
	switch job.Type {
	case "terraformPlan", "terraformPlanDestroy", "terraformApply", "terraformDestroy":
		executionErr = p.executeTerraform(job, workingDir, streamer, &logBuffer)

	case "customScripts", "approval":
		scriptExecutor := script.NewExecutor(job, workingDir, streamer)
		executionErr = scriptExecutor.Execute()

		output := logBuffer.String()
		if executionErr != nil {
			output += "\nError: " + executionErr.Error()
			p.Status.SetCompleted(job, false, output)
		} else if job.Type == "approval" {
			// Approval gate passed: mark this step completed but put the job back
			// in "queue" so the Java API dispatches the next step (apply / destroy).
			// Using SetCompleted here would set job="completed" and cause the Java
			// API to mark all remaining steps as "notExecuted".
			p.Status.SetApprovalCompleted(job, output)
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

	// Prepend terraform binary dir to PATH so after/onFailure scripts can call `terraform` directly
	execDir := filepath.Dir(execPath)
	currentPath := os.Getenv("PATH")
	job.EnvironmentVariables["PATH"] = execDir + ":" + currentPath

	if err := p.generateBackendOverride(job, workingDir); err != nil {
		errMsg := fmt.Sprintf("failed to generate backend override: %v", err)
		p.Status.SetCompleted(job, false, errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	if err := p.generateTerraformCredentials(job, workingDir); err != nil {
		errMsg := fmt.Sprintf("failed to generate terraform credentials: %v", err)
		p.Status.SetCompleted(job, false, errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Notify: approval was given, operation is starting (apply / destroy only)
	if job.Type == "terraformApply" || job.Type == "terraformDestroy" {
		p.notifySlackApproved(job)
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
		p.notifySlackOnFailure(job)

		output := logBuffer.String() + "\nError: " + err.Error()
		if statusErr := p.Status.SetCompleted(job, false, output); statusErr != nil {
			log.Printf("Failed to set completed (failed) status: %v", statusErr)
		}
		return err
	}

	// Execute after scripts
	if err := scriptExec.ExecutePhase("after"); err != nil {
		log.Printf("Warning: after scripts failed: %v", err)
	}

	isPlan := job.Type == "terraformPlan" || job.Type == "terraformPlanDestroy"

	// For plan jobs, parse and store structured plan JSON for UI
	if isPlan {
		p.uploadPlanJSON(job, workingDir, execPath)
	}

	// Upload State and Output
	p.uploadStateAndOutput(job, workingDir)

	// Set final status and send matching Slack notification
	output := logBuffer.String()
	if isPlan && result != nil && result.ExitCode == 2 {
		// Plan has changes → pending approval
		if err := p.Status.SetPending(job, output); err != nil {
			log.Printf("Failed to set pending status: %v", err)
		}
		p.notifySlackPlanPending(job, parsePlanSummary(output))
	} else {
		if err := p.Status.SetCompleted(job, true, output); err != nil {
			log.Printf("Failed to set completed status: %v", err)
		}
		if isPlan {
			// Plan exit 0 → no changes
			p.notifySlackPlanNoChanges(job)
		} else {
			// Apply or Destroy succeeded
			p.notifySlackSuccess(job)
		}
	}

	return nil
}

func (p *JobProcessor) downloadPlanForApply(job *model.TerraformJob, workingDir string) {
	// Plan is stored at a job-level path (no step ID) — matches the upload path
	// used by the plan step. Using the apply step's own ID here would always fail
	// since the plan was created by a different step.
	remotePath := fmt.Sprintf("organization/%s/workspace/%s/job/%s/plan/terraformLibrary.tfplan",
		job.OrganizationId, job.WorkspaceId, job.JobId)

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
