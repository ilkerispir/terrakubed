package executor

import (
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

	statusService := status.NewStatusService(cfg)
	// We only need local storage here because Executor state/output upload uses Terraform remote backend mostly,
	// except if it is using local backend, in which case it uses local Nop.
	storageService, err := storage.NewStorageService("LOCAL")
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
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
