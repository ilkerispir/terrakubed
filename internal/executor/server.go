package executor

import (
	"context"
	"log"
	"os"

	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/executor/core"
	"github.com/ilkerispir/terrakubed/internal/executor/mode/batch"
	"github.com/ilkerispir/terrakubed/internal/executor/mode/online"
	"github.com/ilkerispir/terrakubed/internal/status"
	"github.com/ilkerispir/terrakubed/internal/storage"
)

func Start(cfg *config.Config) {
	log.Println("Terrakube Executor Go - Starting...")

	// Initialize storage service based on configured type
	storageService := initStorage(cfg)

	statusService := status.NewStatusService(cfg, storageService)
	processor := core.NewJobProcessor(cfg, statusService, storageService)

	if cfg.Mode == "BATCH" {
		if cfg.EphemeralJobData == nil {
			log.Fatal("Batch mode selected but no job data provided")
		}
		batch.AdjustAndExecute(cfg.EphemeralJobData, processor)
	} else {
		// Default to Online
		port := os.Getenv("PORT")
		if port == "" {
			port = "8090"
		}
		online.StartServer(port, processor)
	}
}

func initStorage(cfg *config.Config) storage.StorageService {
	var storageService storage.StorageService
	var err error

	storageType := cfg.StorageType
	if storageType == "" {
		storageType = "LOCAL"
	}

	switch storageType {
	case "AWS", "AwsStorageImpl":
		storageService, err = storage.NewAWSStorageService(
			context.TODO(),
			cfg.AwsRegion,
			cfg.AwsBucketName,
			cfg.AzBuilderRegistry,
			cfg.AwsEndpoint,
			cfg.AwsAccessKey,
			cfg.AwsSecretKey,
			cfg.AwsEnableRoleAuth,
		)
	case "AZURE", "AzureStorageImpl":
		storageService, err = storage.NewAzureStorageService(
			cfg.AzureStorageAccountName,
			cfg.AzureStorageAccountKey,
			cfg.AzureStorageContainerName,
			cfg.AzBuilderRegistry,
		)
	case "GCP", "GcpStorageImpl":
		storageService, err = storage.NewGCPStorageService(
			context.TODO(),
			cfg.GcpStorageProjectId,
			cfg.GcpStorageBucketName,
			cfg.GcpStorageCredentials,
			cfg.AzBuilderRegistry,
		)
	default:
		log.Printf("Storage type '%s' not recognized, using NopStorageService", storageType)
		storageService = &storage.NopStorageService{}
	}

	if err != nil {
		log.Printf("Warning: failed to initialize %s storage, falling back to NopStorageService: %v", storageType, err)
		storageService = &storage.NopStorageService{}
	}

	return storageService
}
