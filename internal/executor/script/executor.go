package script

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

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

func (e *Executor) buildEnv() []string {
	env := os.Environ()

	// Add executor-specific env vars
	env = append(env,
		fmt.Sprintf("workingDirectory=%s", e.WorkingDir),
		fmt.Sprintf("organizationId=%s", e.Job.OrganizationId),
		fmt.Sprintf("workspaceId=%s", e.Job.WorkspaceId),
		fmt.Sprintf("jobId=%s", e.Job.JobId),
		fmt.Sprintf("stepId=%s", e.Job.StepId),
		fmt.Sprintf("terraformVersion=%s", e.Job.TerraformVersion),
		fmt.Sprintf("source=%s", e.Job.Source),
		fmt.Sprintf("branch=%s", e.Job.Branch),
		fmt.Sprintf("vcsType=%s", e.Job.VcsType),
		fmt.Sprintf("accessToken=%s", e.Job.AccessToken),
		fmt.Sprintf("terraformOutput=%s", e.Job.TerraformOutput),
	)

	for k, v := range e.Job.EnvironmentVariables {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range e.Job.Variables {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// Execute runs all commands from the CommandList sorted by priority.
func (e *Executor) Execute() error {
	commands := make([]model.Command, len(e.Job.CommandList))
	copy(commands, e.Job.CommandList)
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Priority < commands[j].Priority
	})

	env := e.buildEnv()

	for _, command := range commands {
		if command.Verbose && e.Streamer != nil {
			banner := fmt.Sprintf("\n--- Running script (priority: %d) ---\n%s\n---\n", command.Priority, command.Script)
			e.Streamer.Write([]byte(banner))
		}

		cmd := exec.Command("sh", "-c", command.Script)
		cmd.Dir = e.WorkingDir
		cmd.Env = env

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

// ExecutePhase runs commands matching a specific execution phase.
func (e *Executor) ExecutePhase(phase string) error {
	commands := filterByPhase(e.Job.CommandList, phase)
	if len(commands) == 0 {
		return nil
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Priority < commands[j].Priority
	})

	env := e.buildEnv()

	for _, command := range commands {
		if command.Verbose && e.Streamer != nil {
			banner := fmt.Sprintf("\n--- Running %s script (priority: %d) ---\n", phase, command.Priority)
			e.Streamer.Write([]byte(banner))
		}

		cmd := exec.Command("sh", "-c", command.Script)
		cmd.Dir = e.WorkingDir
		cmd.Env = env

		if e.Streamer != nil {
			cmd.Stdout = e.Streamer
			cmd.Stderr = e.Streamer
		}

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s script execution failed: %w", phase, err)
		}
	}
	return nil
}

func filterByPhase(commands []model.Command, phase string) []model.Command {
	var filtered []model.Command
	for _, cmd := range commands {
		switch phase {
		case "beforeInit":
			if cmd.BeforeInit {
				filtered = append(filtered, cmd)
			}
		case "before":
			if cmd.Before && !cmd.BeforeInit {
				filtered = append(filtered, cmd)
			}
		case "after":
			if cmd.After {
				filtered = append(filtered, cmd)
			}
		case "onFailure":
			if cmd.OnFailure {
				filtered = append(filtered, cmd)
			}
		}
	}
	return filtered
}
