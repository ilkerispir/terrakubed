package workspace

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/git"
	"github.com/ilkerispir/terrakubed/internal/model"
)

type Workspace struct {
	Job        *model.TerraformJob
	WorkingDir string
	apiToken   string // used for downloading remote-content (CLI) uploads
}

func NewWorkspace(job *model.TerraformJob, apiToken string) *Workspace {
	return &Workspace{
		Job:      job,
		apiToken: apiToken,
	}
}

// Setup prepares the working directory.
// For CLI-triggered runs (branch == "remote-content") it downloads and extracts
// the tar.gz that Terraform CLI uploaded to the API, matching the Java executor's
// SetupWorkspaceImpl.prepareWorkspace() behaviour.
// For VCS-backed runs it does the usual git clone.
func (w *Workspace) Setup() (string, error) {
	if w.Job.Branch == "remote-content" {
		return w.setupFromTarGz()
	}

	gitSvc := git.NewService()
	finalDir, err := gitSvc.CloneWorkspace(w.Job.Source, w.Job.Branch, w.Job.VcsType, w.Job.ConnectionType, w.Job.AccessToken, w.Job.Folder, w.Job.JobId)
	if err != nil {
		return "", err
	}

	// WorkingDir keeps track of the temp root so it can be cleaned up
	// finalDir might be inside the temp root (when Folder is set)
	if w.Job.Folder != "" {
		w.WorkingDir = strings.TrimSuffix(finalDir, "/"+w.Job.Folder)
	} else {
		w.WorkingDir = finalDir
	}

	return finalDir, nil
}

// setupFromTarGz handles CLI-uploaded configurations.
// The Java API sets source = "https://<api>/remote/tfe/v2/configuration-versions/<id>/terraformContent.tar.gz"
// and branch = "remote-content" when Terraform CLI uploads local config via the remote backend.
func (w *Workspace) setupFromTarGz() (string, error) {
	tempDir, err := os.MkdirTemp("", "terrakube-cli-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir for CLI upload: %w", err)
	}
	w.WorkingDir = tempDir

	req, err := http.NewRequest(http.MethodGet, w.Job.Source, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build download request for %s: %w", w.Job.Source, err)
	}
	if w.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.apiToken)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download CLI config from %s: %w", w.Job.Source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download CLI config: HTTP %d from %s", resp.StatusCode, w.Job.Source)
	}

	if err := extractTarGz(resp.Body, tempDir); err != nil {
		return "", fmt.Errorf("failed to extract CLI config tar.gz: %w", err)
	}

	// For CLI uploads the Folder field is meaningless: Terraform CLI uploads only the
	// specific directory's contents (not the full repo), so the extracted root IS the
	// working directory. Applying Folder here would point to a non-existent sub-path.
	return tempDir, nil
}

// extractTarGz extracts a gzipped tar archive into destDir.
func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a valid gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(destDir) + string(filepath.Separator)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Resolve target path and guard against path-traversal
		target := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(filepath.Separator), cleanDest) {
			return fmt.Errorf("unsafe tar entry path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.Symlink(header.Linkname, target); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}

	return nil
}

func (w *Workspace) Cleanup() error {
	if w.WorkingDir != "" {
		return os.RemoveAll(w.WorkingDir)
	}
	return nil
}
