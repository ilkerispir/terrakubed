package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitService interface {
	CloneRepository(source, version, vcsType, accessToken, tagPrefix, folder string) (string, error)
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

// setupCredentialURL injects the appropriate credential format based on VCS type.
func setupCredentialURL(source, vcsType, connectionType, accessToken string) string {
	if accessToken == "" || vcsType == "PUBLIC" || strings.HasPrefix(vcsType, "SSH") {
		return source
	}

	if !strings.HasPrefix(source, "https://") {
		return source
	}

	var user string
	switch vcsType {
	case "GITHUB":
		if connectionType == "OAUTH" {
			return strings.Replace(source, "https://", fmt.Sprintf("https://%s@", accessToken), 1)
		}
		user = "x-access-token"
	case "BITBUCKET":
		user = "x-token-auth"
	case "GITLAB":
		user = "oauth2"
	case "AZURE_DEVOPS":
		user = "dummy"
	default:
		user = "oauth2"
	}

	return strings.Replace(source, "https://", fmt.Sprintf("https://%s:%s@", user, accessToken), 1)
}

// setupSSHEnv prepares SSH environment for git clone when using SSH keys.
func setupSSHEnv(vcsType, accessToken, tempDir string) ([]string, func(), error) {
	cleanup := func() {}
	env := os.Environ()

	if !strings.HasPrefix(vcsType, "SSH") || accessToken == "" {
		return env, cleanup, nil
	}

	parts := strings.SplitN(vcsType, "~", 2)
	keyName := "id_rsa"
	if len(parts) == 2 && parts[1] != "" {
		keyName = parts[1]
	}

	sshDir := filepath.Join(tempDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, cleanup, fmt.Errorf("failed to create SSH dir: %w", err)
	}

	keyPath := filepath.Join(sshDir, keyName)
	if err := os.WriteFile(keyPath, []byte(accessToken), 0600); err != nil {
		return nil, cleanup, fmt.Errorf("failed to write SSH key: %w", err)
	}

	cleanup = func() { os.RemoveAll(sshDir) }

	sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", keyPath)
	env = append(env, "GIT_SSH_COMMAND="+sshCmd)

	return env, cleanup, nil
}

func (s *Service) CloneRepository(source, version, vcsType, accessToken, tagPrefix, folder string) (string, error) {
	tempDir, err := os.MkdirTemp("", "terrakube-registry")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	repoURL := setupCredentialURL(source, vcsType, "", accessToken)

	env, sshCleanup, err := setupSSHEnv(vcsType, accessToken, tempDir)
	if err != nil {
		return "", err
	}
	defer sshCleanup()

	// Try tag with "v" prefix first, then without
	tag := tagPrefix + "v" + version
	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", tag, repoURL, tempDir)
	cloneCmd.Env = env

	if _, err := cloneCmd.CombinedOutput(); err != nil {
		tag = tagPrefix + version
		os.RemoveAll(tempDir)
		tempDir, _ = os.MkdirTemp("", "terrakube-registry")
		cloneCmd = exec.Command("git", "clone", "--depth", "1", "--branch", tag, repoURL, tempDir)
		cloneCmd.Env = env
		if output, err := cloneCmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone failed for tag %s: %s: %w", tag, string(output), err)
		}
	}

	if folder != "" {
		return filepath.Join(tempDir, folder), nil
	}

	return tempDir, nil
}

func (s *Service) CloneWorkspace(source, branch, vcsType, connectionType, accessToken, folder string, jobId string) (string, error) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("terrakube-job-%s", jobId))
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	repoURL := setupCredentialURL(source, vcsType, connectionType, accessToken)

	env, sshCleanup, err := setupSSHEnv(vcsType, accessToken, tempDir)
	if err != nil {
		return "", err
	}
	defer sshCleanup()

	cmdArgs := []string{"clone", "--depth", "1"}
	if branch != "" {
		cmdArgs = append(cmdArgs, "--branch", branch)
	}
	cmdArgs = append(cmdArgs, repoURL, tempDir)

	cloneCmd := exec.Command("git", cmdArgs...)
	cloneCmd.Env = env

	if output, err := cloneCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone failed: %s: %w", string(output), err)
	}

	finalDir := tempDir
	if folder != "" {
		finalDir = filepath.Join(tempDir, folder)
	}

	return finalDir, nil
}
