package status

import (
	"fmt"
	"log"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/auth"
	"github.com/ilkerispir/terrakubed/internal/client"
	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/model"
	"github.com/ilkerispir/terrakubed/internal/storage"
)

type StatusService interface {
	SetRunning(job *model.TerraformJob) error
	SetCompleted(job *model.TerraformJob, success bool, output string) error
	SetPending(job *model.TerraformJob, output string) error
	UpdateCommitId(job *model.TerraformJob, commitId string) error
	CreateHistory(job *model.TerraformJob, stateURL string) error
}

type Service struct {
	client  *client.TerrakubeClient
	storage storage.StorageService
	apiUrl  string
}

func NewStatusService(cfg *config.Config, storageService storage.StorageService) *Service {
	token, err := auth.GenerateTerrakubeToken(cfg.InternalSecret)
	if err != nil {
		log.Printf("Warning: failed to generate Terrakube token for API requests: %v", err)
	}
	return &Service{
		client:  client.NewTerrakubeClient(cfg.AzBuilderApiUrl, token),
		storage: storageService,
		apiUrl:  cfg.AzBuilderApiUrl,
	}
}

// getOutputPath returns the API URL path where the UI will fetch log output from.
// This matches the Java TerraformOutputPathService.getOutputPath() format.
func (s *Service) getOutputPath(orgId, jobId, stepId string) string {
	base := strings.TrimRight(s.apiUrl, "/")
	return fmt.Sprintf("%s/tfoutput/v1/organization/%s/job/%s/step/%s", base, orgId, jobId, stepId)
}

func (s *Service) SetRunning(job *model.TerraformJob) error {
	// Update step status to "running" with output path so the UI can start polling for logs
	outputPath := s.getOutputPath(job.OrganizationId, job.JobId, job.StepId)
	if err := s.client.UpdateStepStatus(job.OrganizationId, job.JobId, job.StepId, "running", outputPath); err != nil {
		log.Printf("Failed to update step to running: %v", err)
	}
	return s.client.UpdateJobStatus(job.OrganizationId, job.JobId, "running", "")
}

func (s *Service) SetCompleted(job *model.TerraformJob, success bool, output string) error {
	// Upload log output to storage
	outputPath := s.saveOutput(job.OrganizationId, job.JobId, job.StepId, output)

	status := "completed"
	if !success {
		status = "failed"
	}
	if err := s.client.UpdateStepStatus(job.OrganizationId, job.JobId, job.StepId, status, outputPath); err != nil {
		return fmt.Errorf("failed to update step status: %w", err)
	}
	return s.client.UpdateJobStatus(job.OrganizationId, job.JobId, status, "")
}

func (s *Service) SetPending(job *model.TerraformJob, output string) error {
	outputPath := s.saveOutput(job.OrganizationId, job.JobId, job.StepId, output)

	if err := s.client.UpdateStepStatus(job.OrganizationId, job.JobId, job.StepId, "pending", outputPath); err != nil {
		return fmt.Errorf("failed to update step status: %w", err)
	}
	return s.client.UpdateJobStatus(job.OrganizationId, job.JobId, "pending", "")
}

func (s *Service) UpdateCommitId(job *model.TerraformJob, commitId string) error {
	return s.client.UpdateJobCommitId(job.OrganizationId, job.JobId, commitId)
}

func (s *Service) CreateHistory(job *model.TerraformJob, stateURL string) error {
	return s.client.CreateHistory(job.OrganizationId, job.WorkspaceId, stateURL)
}

// saveOutput uploads the terraform log output to object storage and returns the output URL path.
// Matches the Java AwsTerraformStateImpl.saveOutput() behavior.
func (s *Service) saveOutput(orgId, jobId, stepId, output string) string {
	remotePath := fmt.Sprintf("tfoutput/%s/%s/%s.tfoutput", orgId, jobId, stepId)
	if err := s.storage.UploadFile(remotePath, strings.NewReader(output)); err != nil {
		log.Printf("Warning: failed to upload log output to storage: %v", err)
	}
	return s.getOutputPath(orgId, jobId, stepId)
}
