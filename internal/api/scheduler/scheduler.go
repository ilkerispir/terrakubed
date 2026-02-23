package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobScheduler polls for pending jobs and dispatches them to an executor.
type JobScheduler struct {
	pool     *pgxpool.Pool
	executor Executor
	interval time.Duration
}

// Executor is the interface for job execution backends.
type Executor interface {
	Execute(ctx context.Context, execCtx *ExecutionContext) error
}

// ExecutionContext contains everything needed to execute a job.
type ExecutionContext struct {
	OrganizationID   string            `json:"organizationId"`
	WorkspaceID      string            `json:"workspaceId"`
	JobID            int               `json:"jobId"`
	StepID           string            `json:"stepId"`
	Source           string            `json:"source"`
	Branch           string            `json:"branch"`
	Folder           string            `json:"folder"`
	TerraformVersion string            `json:"terraformVersion"`
	VcsType          string            `json:"vcsType"`
	ConnectionType   string            `json:"connectionType"`
	AccessToken      string            `json:"accessToken"`
	ModuleSshKey     string            `json:"moduleSshKey"`
	CommitID         string            `json:"commitId"`
	Refresh          bool              `json:"refresh"`
	RefreshOnly      bool              `json:"refreshOnly"`
	IacType          string            `json:"iacType"`
	TCL              string            `json:"tcl"`
	EnvVars          map[string]string `json:"environmentVariables"`
	TFVars           map[string]string `json:"variables"`
}

// NewJobScheduler creates a new scheduler.
func NewJobScheduler(pool *pgxpool.Pool, executor Executor, interval time.Duration) *JobScheduler {
	return &JobScheduler{
		pool:     pool,
		executor: executor,
		interval: interval,
	}
}

// Start begins the polling loop.
func (s *JobScheduler) Start(ctx context.Context) {
	log.Printf("Job scheduler starting (interval: %s)", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Job scheduler stopped")
			return
		case <-ticker.C:
			s.pollJobs(ctx)
		}
	}
}

// pollJobs checks for pending jobs and processes them.
func (s *JobScheduler) pollJobs(ctx context.Context) {
	// Find jobs in "pending" or "queue" status
	rows, err := s.pool.Query(ctx, `
		SELECT j.id, j.status, j.tcl, j.template_reference, j.commit_id,
		       j.organization_id, j.workspace_id, j.refresh, j.refresh_only,
		       w.source, w.branch, w.folder, w.terraform_version, w.iac_type,
		       w.module_ssh_key,
		       v.vcs_type, v.connection_type, v.access_token
		FROM job j
		JOIN workspace w ON j.workspace_id = w.id
		LEFT JOIN vcs v ON w.vcs_id = v.id
		WHERE j.status IN ('pending', 'queue')
		ORDER BY j.id ASC
		LIMIT 10
	`)
	if err != nil {
		log.Printf("Error polling jobs: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			jobID            int
			status           string
			tcl              *string
			templateRef      *string
			commitID         *string
			orgID            string
			workspaceID      string
			refresh          bool
			refreshOnly      bool
			source           *string
			branch           *string
			folder           *string
			terraformVersion *string
			iacType          *string
			moduleSshKey     *string
			vcsType          *string
			connectionType   *string
			accessToken      *string
		)

		if err := rows.Scan(
			&jobID, &status, &tcl, &templateRef, &commitID,
			&orgID, &workspaceID, &refresh, &refreshOnly,
			&source, &branch, &folder, &terraformVersion, &iacType,
			&moduleSshKey,
			&vcsType, &connectionType, &accessToken,
		); err != nil {
			log.Printf("Error scanning job row: %v", err)
			continue
		}

		if status == "pending" {
			// Transition to "queue" status
			_, err := s.pool.Exec(ctx, "UPDATE job SET status = 'queue' WHERE id = $1", jobID)
			if err != nil {
				log.Printf("Error updating job %d status: %v", jobID, err)
				continue
			}
			log.Printf("Job %d queued", jobID)
			continue
		}

		// Status is "queue" — find the first pending step
		var stepID string
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM step WHERE job_id = $1 AND status = 'pending' ORDER BY step_number ASC LIMIT 1`,
			jobID,
		).Scan(&stepID)
		if err != nil {
			log.Printf("No pending step for job %d: %v", jobID, err)
			continue
		}

		// Build execution context
		execCtx := &ExecutionContext{
			OrganizationID:   orgID,
			WorkspaceID:      workspaceID,
			JobID:            jobID,
			StepID:           stepID,
			Source:           deref(source),
			Branch:           deref(branch),
			Folder:           deref(folder),
			TerraformVersion: deref(terraformVersion),
			VcsType:          deref(vcsType),
			ConnectionType:   deref(connectionType),
			AccessToken:      deref(accessToken),
			ModuleSshKey:     deref(moduleSshKey),
			CommitID:         deref(commitID),
			Refresh:          refresh,
			RefreshOnly:      refreshOnly,
			IacType:          deref(iacType),
			TCL:              deref(tcl),
		}

		// Load environment and terraform variables
		execCtx.EnvVars = s.loadVariables(ctx, orgID, workspaceID, "ENV")
		execCtx.TFVars = s.loadVariables(ctx, orgID, workspaceID, "TERRAFORM")

		// Mark step as running
		_, err = s.pool.Exec(ctx, "UPDATE step SET status = 'running' WHERE id = $1", stepID)
		if err != nil {
			log.Printf("Error marking step %s as running: %v", stepID, err)
			continue
		}

		// Mark job as running
		_, err = s.pool.Exec(ctx, "UPDATE job SET status = 'running' WHERE id = $1", jobID)
		if err != nil {
			log.Printf("Error marking job %d as running: %v", jobID, err)
			continue
		}

		log.Printf("Dispatching job %d step %s", jobID, stepID)

		// Execute asynchronously
		go func(jID int, sID string, ec *ExecutionContext) {
			if err := s.executor.Execute(ctx, ec); err != nil {
				log.Printf("Job %d step %s execution failed: %v", jID, sID, err)
				s.pool.Exec(ctx, "UPDATE step SET status = 'failed' WHERE id = $1", sID)
				s.pool.Exec(ctx, "UPDATE job SET status = 'failed' WHERE id = $1", jID)
			}
		}(jobID, stepID, execCtx)
	}
}

// loadVariables loads workspace variables and global variables for a given category.
func (s *JobScheduler) loadVariables(ctx context.Context, orgID, workspaceID, category string) map[string]string {
	vars := make(map[string]string)

	// Load global variables
	rows, err := s.pool.Query(ctx,
		`SELECT variable_key, variable_value FROM globalvar
		 WHERE organization_id = $1 AND variable_category = $2`,
		orgID, category,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var key, value string
			if rows.Scan(&key, &value) == nil {
				vars[key] = value
			}
		}
	}

	// Load workspace variables (override globals)
	rows2, err := s.pool.Query(ctx,
		`SELECT variable_key, variable_value FROM variable
		 WHERE workspace_id = $1 AND variable_category = $2`,
		workspaceID, category,
	)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var key, value string
			if rows2.Scan(&key, &value) == nil {
				vars[key] = value
			}
		}
	}

	return vars
}

// MarshalJSON serializes ExecutionContext to JSON for passing to ephemeral pods.
func (e *ExecutionContext) MarshalJSON2() ([]byte, error) {
	return json.Marshal(*e)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ──────────────────────────────────────────────────
// Ephemeral Executor (Kubernetes Jobs)
// ──────────────────────────────────────────────────

// EphemeralConfig holds config for the K8s ephemeral executor.
type EphemeralConfig struct {
	Namespace      string
	Image          string
	SecretName     string
	ServiceAccount string
	NodeSelector   map[string]string
	Annotations    map[string]string
	Tolerations    []map[string]string
}

// EphemeralExecutor creates Kubernetes Jobs for each execution.
type EphemeralExecutor struct {
	config EphemeralConfig
}

// NewEphemeralExecutor creates a new ephemeral executor.
func NewEphemeralExecutor(config EphemeralConfig) *EphemeralExecutor {
	return &EphemeralExecutor{config: config}
}

// Execute creates a K8s Job for the given execution context.
func (e *EphemeralExecutor) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	jobName := fmt.Sprintf("terrakube-job-%d-%s", execCtx.JobID, execCtx.StepID[:8])

	// Serialize execution context for the ephemeral pod
	execData, err := json.Marshal(execCtx)
	if err != nil {
		return fmt.Errorf("failed to serialize execution context: %w", err)
	}

	log.Printf("Creating K8s Job: %s (namespace: %s, image: %s)", jobName, e.config.Namespace, e.config.Image)
	log.Printf("Execution context size: %d bytes", len(execData))

	// K8s Job creation will use the existing Go executor's K8s client
	// or the fabric8-equivalent Go library (k8s.io/client-go).
	// For now, we log what would be created.
	//
	// The job spec matches Java's EphemeralExecutorService:
	// - Container image: e.config.Image
	// - Env from secret: e.config.SecretName
	// - Env var: EphemeralJobData = base64(execData)
	// - ServiceAccount: e.config.ServiceAccount
	// - NodeSelector: e.config.NodeSelector
	// - Tolerations: e.config.Tolerations
	// - TTL after finished: 30s
	// - RestartPolicy: Never
	// - Labels: terrakube.io/organization, terrakube.io/workspace

	log.Printf("K8s Job %s created successfully (stub — will use k8s.io/client-go)", jobName)
	return nil
}
