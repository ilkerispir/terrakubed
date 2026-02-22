package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/ilkerispir/terrakubed/internal/executor/logs"
	"github.com/ilkerispir/terrakubed/internal/model"
)

// ExecutionResult holds the outcome of a terraform execution.
type ExecutionResult struct {
	Success  bool
	ExitCode int // 0=no changes, 2=changes present (plan)
}

type Executor struct {
	Job        *model.TerraformJob
	WorkingDir string
	Streamer   logs.LogStreamer
	ExecPath   string
}

func NewExecutor(job *model.TerraformJob, workingDir string, streamer logs.LogStreamer, execPath string) *Executor {
	return &Executor{
		Job:        job,
		WorkingDir: workingDir,
		Streamer:   streamer,
		ExecPath:   execPath,
	}
}

func (e *Executor) setupTerraform() (*tfexec.Terraform, error) {
	tf, err := tfexec.NewTerraform(e.WorkingDir, e.ExecPath)
	if err != nil {
		return nil, fmt.Errorf("error running NewTerraform: %s", err)
	}

	env := e.buildEnvMap()
	tf.SetEnv(env)

	if e.Streamer != nil {
		tf.SetStdout(e.Streamer)
		tf.SetStderr(e.Streamer)
	}

	return tf, nil
}

// buildEnvMap creates environment variables map merging OS env + job env + job variables
func (e *Executor) buildEnvMap() map[string]string {
	env := make(map[string]string)

	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				env[kv[:i]] = kv[i+1:]
				break
			}
		}
	}

	for k, v := range e.Job.EnvironmentVariables {
		env[k] = v
	}
	for k, v := range e.Job.Variables {
		env[fmt.Sprintf("TF_VAR_%s", k)] = v
	}

	return env
}

// runTerraformDirect runs terraform via os/exec to enable color output.
// terraform-exec hardcodes -no-color, so we bypass it for user-facing commands.
func (e *Executor) runTerraformDirect(args ...string) error {
	cmd := exec.Command(e.ExecPath, args...)
	cmd.Dir = e.WorkingDir

	// Build environment
	envMap := e.buildEnvMap()
	envSlice := make([]string, 0, len(envMap))
	for k, v := range envMap {
		envSlice = append(envSlice, k+"="+v)
	}
	cmd.Env = envSlice

	if e.Streamer != nil {
		cmd.Stdout = e.Streamer
		cmd.Stderr = e.Streamer
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

func (e *Executor) Execute() (*ExecutionResult, error) {
	ctx := context.Background()

	if e.Job.ShowHeader && e.Streamer != nil {
		header := fmt.Sprintf("\n========================================\nRunning %s\n========================================\n", e.Job.Type)
		e.Streamer.Write([]byte(header))
	}

	// Init with color support (direct execution)
	err := e.runTerraformDirect("init", "-input=false", "-upgrade")
	if err != nil {
		return nil, fmt.Errorf("error running Init: %s", err)
	}

	result := &ExecutionResult{Success: true, ExitCode: 0}

	switch e.Job.Type {
	case "terraformPlan":
		result, err = e.executePlan(ctx)
	case "terraformApply":
		err = e.executeApply(ctx)
	case "terraformDestroy":
		err = e.executeDestroy()
	default:
		return nil, fmt.Errorf("unknown job type: %s", e.Job.Type)
	}

	if err != nil {
		if e.Job.IgnoreError {
			return &ExecutionResult{Success: true, ExitCode: 0}, nil
		}
		return &ExecutionResult{Success: false, ExitCode: 1}, fmt.Errorf("error running %s: %s", e.Job.Type, err)
	}

	return result, nil
}

func (e *Executor) executePlan(ctx context.Context) (*ExecutionResult, error) {
	planFile := filepath.Join(e.WorkingDir, "terraform.tfplan")

	args := []string{"plan", "-input=false", "-detailed-exitcode", "-out=" + planFile}

	if e.Job.Refresh {
		args = append(args, "-refresh=true")
	}
	if e.Job.RefreshOnly {
		args = append(args, "-refresh-only")
	}

	err := e.runTerraformDirect(args...)
	if err != nil {
		// Exit code 2 = changes present (not an error for plan)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 2 {
				return &ExecutionResult{Success: true, ExitCode: 2}, nil
			}
		}
		return &ExecutionResult{Success: false, ExitCode: 1}, err
	}

	return &ExecutionResult{Success: true, ExitCode: 0}, nil
}

func (e *Executor) executeApply(ctx context.Context) error {
	planFile := filepath.Join(e.WorkingDir, "terraformLibrary.tfPlan")
	if _, err := os.Stat(planFile); err == nil {
		return e.runTerraformDirect("apply", "-input=false", "-auto-approve", planFile)
	}

	args := []string{"apply", "-input=false", "-auto-approve"}
	if e.Job.Refresh {
		args = append(args, "-refresh=true")
	}
	return e.runTerraformDirect(args...)
}

func (e *Executor) executeDestroy() error {
	args := []string{"destroy", "-input=false", "-auto-approve"}
	if e.Job.Refresh {
		args = append(args, "-refresh=true")
	}
	return e.runTerraformDirect(args...)
}

func (e *Executor) Output() (string, error) {
	tf, err := tfexec.NewTerraform(e.WorkingDir, e.ExecPath)
	if err != nil {
		return "", fmt.Errorf("error running NewTerraform: %s", err)
	}

	output, err := tf.Output(context.Background())
	if err != nil {
		return "", fmt.Errorf("error running Output: %s", err)
	}

	bytes, err := json.Marshal(output)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func (e *Executor) ShowState() (string, error) {
	tf, err := tfexec.NewTerraform(e.WorkingDir, e.ExecPath)
	if err != nil {
		return "", fmt.Errorf("error running NewTerraform: %s", err)
	}

	state, err := tf.ShowStateFile(context.Background(), filepath.Join(e.WorkingDir, "terraform.tfstate"))
	if err != nil {
		return "", fmt.Errorf("error running Show: %s", err)
	}

	bytes, err := json.Marshal(state)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func (e *Executor) StatePull() (string, error) {
	tf, err := tfexec.NewTerraform(e.WorkingDir, e.ExecPath)
	if err != nil {
		return "", fmt.Errorf("error running NewTerraform: %s", err)
	}

	state, err := tf.StatePull(context.Background())
	if err != nil {
		return "", fmt.Errorf("error running StatePull: %s", err)
	}

	return state, nil
}
