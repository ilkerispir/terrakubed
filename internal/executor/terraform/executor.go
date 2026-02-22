package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/ilkerispir/terrakubed/internal/executor/logs"
	"github.com/ilkerispir/terrakubed/internal/model"
)

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

func (e *Executor) Execute() error {
	tf, err := tfexec.NewTerraform(e.WorkingDir, e.ExecPath)
	if err != nil {
		return fmt.Errorf("error running NewTerraform: %s", err)
	}

	// Set Environment Variables
	env := make(map[string]string)

	// Parse OS environment
	for _, e := range os.Environ() {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				env[e[:i]] = e[i+1:]
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

	// Set Log Streaming
	if e.Streamer != nil {
		tf.SetStdout(e.Streamer)
		tf.SetStderr(e.Streamer)
	}

	ctx := context.Background()

	// Init
	err = tf.Init(ctx, tfexec.Upgrade(true))
	if err != nil {
		return fmt.Errorf("error running Init: %s", err)
	}

	switch e.Job.Type {
	case "terraformPlan":
		_, err = tf.Plan(ctx)
	case "terraformApply":
		// Apply should probably use the plan if available?
		// For now standard apply
		err = tf.Apply(ctx)
	case "terraformDestroy":
		err = tf.Destroy(ctx)
	default:
		return fmt.Errorf("unknown job type: %s", e.Job.Type)
	}

	if err != nil {
		return fmt.Errorf("error running %s: %s", e.Job.Type, err)
	}

	return nil
}

func (e *Executor) Output() (string, error) {
	tf, err := tfexec.NewTerraform(e.WorkingDir, e.ExecPath)
	if err != nil {
		return "", fmt.Errorf("error running NewTerraform: %s", err)
	}

	// Set Log Streaming (Optional for Output)
	if e.Streamer != nil {
		tf.SetStdout(e.Streamer)
		tf.SetStderr(e.Streamer)
	}

	output, err := tf.Output(context.Background())
	if err != nil {
		return "", fmt.Errorf("error running Output: %s", err)
	}

	// Convert outputs to JSON string manually or use tfjson.Format
	// tfexec Output returns map[string]tfjson.StateOutput
	// We need raw JSON string often?
	// tfexec.Output internally parses JSON.
	// To get raw JSON string, we might need a custom command or re-marshal?
	// Terrakube Java expects raw JSON string? "terraformJob.setTerraformOutput(jsonOutput.toString())"
	// Let's re-marshal for simplicity

	bytes, err := json.Marshal(output)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}
