package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

	tf.SetEnv(env)

	if e.Streamer != nil {
		tf.SetStdout(e.Streamer)
		tf.SetStderr(e.Streamer)
	}

	return tf, nil
}

func (e *Executor) Execute() (*ExecutionResult, error) {
	tf, err := e.setupTerraform()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()

	if e.Job.ShowHeader && e.Streamer != nil {
		header := fmt.Sprintf("\n========================================\nRunning %s\n========================================\n", e.Job.Type)
		e.Streamer.Write([]byte(header))
	}

	err = tf.Init(ctx, tfexec.Upgrade(true))
	if err != nil {
		return nil, fmt.Errorf("error running Init: %s", err)
	}

	result := &ExecutionResult{Success: true, ExitCode: 0}

	switch e.Job.Type {
	case "terraformPlan":
		result, err = e.executePlan(ctx, tf, false)
	case "terraformApply":
		err = e.executeApply(ctx, tf)
	case "terraformDestroy":
		err = tf.Destroy(ctx, e.buildDestroyOptions()...)
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

func (e *Executor) executePlan(ctx context.Context, tf *tfexec.Terraform, isDestroy bool) (*ExecutionResult, error) {
	planFile := filepath.Join(e.WorkingDir, "terraform.tfplan")

	opts := []tfexec.PlanOption{
		tfexec.Out(planFile),
	}

	if e.Job.Refresh {
		opts = append(opts, tfexec.Refresh(true))
	}
	if e.Job.RefreshOnly {
		opts = append(opts, tfexec.RefreshOnly(true))
	}
	if isDestroy {
		opts = append(opts, tfexec.Destroy(true))
	}

	hasChanges, err := tf.Plan(ctx, opts...)
	if err != nil {
		return &ExecutionResult{Success: false, ExitCode: 1}, err
	}

	if hasChanges {
		return &ExecutionResult{Success: true, ExitCode: 2}, nil
	}

	return &ExecutionResult{Success: true, ExitCode: 0}, nil
}

func (e *Executor) executeApply(ctx context.Context, tf *tfexec.Terraform) error {
	planFile := filepath.Join(e.WorkingDir, "terraformLibrary.tfPlan")
	if _, err := os.Stat(planFile); err == nil {
		return tf.Apply(ctx, tfexec.DirOrPlan(planFile))
	}

	return tf.Apply(ctx, e.buildApplyOptions()...)
}

func (e *Executor) buildApplyOptions() []tfexec.ApplyOption {
	var opts []tfexec.ApplyOption
	if e.Job.Refresh {
		opts = append(opts, tfexec.Refresh(true))
	}
	return opts
}

func (e *Executor) buildDestroyOptions() []tfexec.DestroyOption {
	var opts []tfexec.DestroyOption
	if e.Job.Refresh {
		opts = append(opts, tfexec.Refresh(true))
	}
	return opts
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
