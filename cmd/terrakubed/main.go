package main

import (
	"log"
	"os"
	"strconv"
	"sync"

	api "github.com/ilkerispir/terrakubed/internal/api"
	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/executor"
	"github.com/ilkerispir/terrakubed/internal/registry"
)

func main() {
	serviceType := os.Getenv("SERVICE_TYPE")
	if serviceType == "" {
		// Default to running all services for easy local development
		serviceType = "all"
	}

	// Automatically set PORT based on SERVICE_TYPE if it is not provided
	if os.Getenv("PORT") == "" {
		if serviceType == "executor" {
			os.Setenv("PORT", "8090")
		} else if serviceType == "registry" {
			os.Setenv("PORT", "8075")
		}
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting Terrakubed (Service Type: %s)\n", serviceType)

	var wg sync.WaitGroup

	switch serviceType {
	case "api":
		wg.Add(1)
		go func() {
			defer wg.Done()
			startAPI(cfg)
		}()
	case "registry":
		wg.Add(1)
		go func() {
			defer wg.Done()
			startRegistry(cfg)
		}()
	case "executor":
		wg.Add(1)
		go func() {
			defer wg.Done()
			startExecutor(cfg)
		}()
	case "all":
		wg.Add(3)
		go func() {
			defer wg.Done()
			startAPI(cfg)
		}()
		go func() {
			defer wg.Done()
			startRegistry(cfg)
		}()
		go func() {
			defer wg.Done()
			startExecutor(cfg)
		}()
	default:
		log.Fatalf("Unknown SERVICE_TYPE: %s. Supported values are: api, registry, executor, all", serviceType)
	}

	wg.Wait()
}

func startAPI(cfg *config.Config) {
	log.Println("API service is starting...")

	port, err := strconv.Atoi(cfg.ApiPort)
	if err != nil {
		port = 8080
	}

	apiConfig := api.Config{
		DatabaseURL:    cfg.DatabaseURL,
		Port:           port,
		Hostname:       cfg.Hostname,
		DexIssuerURI:   cfg.IssuerUri,
		PatSecret:      cfg.PatSecret,
		InternalSecret: cfg.InternalSecret,
		OwnerGroup:     cfg.OwnerGroup,
		UIURL:          cfg.TerrakubeUiURL,
		StorageType:    cfg.StorageType,
	}

	server, err := api.NewServer(apiConfig)
	if err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}
	defer server.Close()

	if err := server.Start(); err != nil {
		log.Fatalf("API server failed: %v", err)
	}
}

func startRegistry(cfg *config.Config) {
	log.Println("Registry service is starting...")
	registry.Start(cfg)
}

func startExecutor(cfg *config.Config) {
	log.Println("Executor service is starting...")
	executor.Start(cfg)
}
