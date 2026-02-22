package workspace

import (
	"os"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/git"
	"github.com/ilkerispir/terrakubed/internal/model"
)

type Workspace struct {
	Job        *model.TerraformJob
	WorkingDir string
}

func NewWorkspace(job *model.TerraformJob) *Workspace {
	return &Workspace{
		Job: job,
	}
}

func (w *Workspace) Setup() (string, error) {
	gitSvc := git.NewService()
	finalDir, err := gitSvc.CloneWorkspace(w.Job.Source, w.Job.Branch, w.Job.VcsType, w.Job.AccessToken, w.Job.Folder, w.Job.JobId)
	if err != nil {
		return "", err
	}

	// WorkingDir keeps track of the temp root so it can be cleaned up
	// finalDir might be inside the temp root
	if w.Job.Folder != "" {
		w.WorkingDir = strings.TrimSuffix(finalDir, "/"+w.Job.Folder)
	} else {
		w.WorkingDir = finalDir
	}

	return finalDir, nil
}

func (w *Workspace) Cleanup() error {
	if w.WorkingDir != "" {
		return os.RemoveAll(w.WorkingDir)
	}
	return nil
}
