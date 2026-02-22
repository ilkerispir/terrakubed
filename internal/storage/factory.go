package storage

import (
	"fmt"
)

// Factory to create storage service
func NewStorageService(storageType string) (StorageService, error) {
	switch storageType {
	case "AWS", "AwsStorageImpl":
		// Assume the Registry's NewAWSStorageService is used, however it requires config params.
		// Wait, the executor will probably need to re-initialize it or we initialize once in main?
		// For now we will return Nop since we only execute local operations mostly for executor,
		// BUT the worker needs to download state.
		// Actually, executor previously called `NewAWSStorageService()` using `os.Getenv()`.
		// Since we unified config, it's better to pass Config down, or just return an error if we try to initialize it here without config.
		return nil, fmt.Errorf("factory initialization for AWS/AZURE/GCP without config is deprecated, use explicit constructor")
	case "LOCAL", "local", "":
		return &NopStorageService{}, nil
	default:
		return nil, fmt.Errorf("unknown storage type: %s", storageType)
	}
}
