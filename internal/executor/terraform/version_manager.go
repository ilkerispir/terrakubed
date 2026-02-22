package terraform

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
)

type VersionManager struct {
	CacheDir string
}

func NewVersionManager() *VersionManager {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Failed to get user home dir, using /tmp: %v", err)
		homeDir = "/tmp"
	}
	cacheDir := filepath.Join(homeDir, ".terrakube", "terraform-versions")

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Printf("Failed to create cache dir: %v", err)
	}

	return &VersionManager{
		CacheDir: cacheDir,
	}
}

func (vm *VersionManager) Install(ver string, tofu bool) (string, error) {
	if tofu {
		return vm.installTofu(ver)
	}
	return vm.installTerraform(ver)
}

func (vm *VersionManager) installTerraform(ver string) (string, error) {
	ctx := context.Background()

	_, err := version.NewVersion(ver)
	if err != nil {
		return "", fmt.Errorf("invalid terraform version %s: %w", ver, err)
	}

	log.Printf("Locating Terraform version %s...", ver)

	installer := &releases.ExactVersion{
		Product:    product.Terraform,
		Version:    version.Must(version.NewVersion(ver)),
		InstallDir: vm.CacheDir,
	}

	execPath, err := installer.Install(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to install terraform %s: %w", ver, err)
	}

	log.Printf("Terraform %s found at: %s", ver, execPath)
	return execPath, nil
}

func (vm *VersionManager) installTofu(ver string) (string, error) {
	tofuDir := filepath.Join(vm.CacheDir, "tofu")
	if err := os.MkdirAll(tofuDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create tofu dir: %w", err)
	}

	execPath := filepath.Join(tofuDir, fmt.Sprintf("tofu-%s", ver), "tofu")
	if _, err := os.Stat(execPath); err == nil {
		log.Printf("OpenTofu %s found at: %s", ver, execPath)
		return execPath, nil
	}

	log.Printf("Installing OpenTofu version %s...", ver)

	installDir := filepath.Join(tofuDir, fmt.Sprintf("tofu-%s", ver))
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install dir: %w", err)
	}

	url := fmt.Sprintf("https://github.com/opentofu/opentofu/releases/download/v%s/tofu_%s_%s_%s.zip", ver, ver, runtime.GOOS, runtime.GOARCH)
	zipPath := filepath.Join(installDir, "tofu.zip")

	cmd := exec.Command("curl", "-sL", "-o", zipPath, url)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to download OpenTofu %s: %s: %w", ver, string(output), err)
	}

	cmd = exec.Command("unzip", "-o", "-d", installDir, zipPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to extract OpenTofu %s: %s: %w", ver, string(output), err)
	}
	os.Remove(zipPath)

	if err := os.Chmod(execPath, 0755); err != nil {
		return "", fmt.Errorf("failed to chmod tofu: %w", err)
	}

	log.Printf("OpenTofu %s installed at: %s", ver, execPath)
	return execPath, nil
}
