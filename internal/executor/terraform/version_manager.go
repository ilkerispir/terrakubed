package terraform

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
)

type VersionManager struct {
	CacheDir string
}

func NewVersionManager() *VersionManager {
	// Use a dedicated directory for terraform binaries
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

func (vm *VersionManager) Install(ver string) (string, error) {
	ctx := context.Background()

	// Parse version to ensure it's valid
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
