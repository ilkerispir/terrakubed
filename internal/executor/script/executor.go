package script

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ilkerispir/terrakubed/internal/executor/logs"
	"github.com/ilkerispir/terrakubed/internal/model"
)

type Executor struct {
	Job        *model.TerraformJob
	WorkingDir string
	Streamer   logs.LogStreamer
}

func NewExecutor(job *model.TerraformJob, workingDir string, streamer logs.LogStreamer) *Executor {
	return &Executor{
		Job:        job,
		WorkingDir: workingDir,
		Streamer:   streamer,
	}
}

func (e *Executor) Execute() error {
	for _, command := range e.Job.CommandList {
		cmd := exec.Command("sh", "-c", command.Script)
		cmd.Dir = e.WorkingDir
		cmd.Env = os.Environ() // TODO: Inject job vars

		if e.Streamer != nil {
			cmd.Stdout = e.Streamer
			cmd.Stderr = e.Streamer
		}

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("script execution failed: %s: %w", command.Script, err)
		}
	}
	return nil
}
