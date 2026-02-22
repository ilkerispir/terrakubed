package main

import (
	"log"
	"os"
	"sync"

	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/registry"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	serviceType := os.Getenv("SERVICE_TYPE")
	if serviceType == "" {
		// Default to running all services for easy local development
		serviceType = "all"
	}

	log.Printf("Starting Terrakubed (Service Type: %s)\n", serviceType)

	var wg sync.WaitGroup

	switch serviceType {
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
		wg.Add(2)
		go func() {
			defer wg.Done()
			startRegistry(cfg)
		}()
		go func() {
			defer wg.Done()
			startExecutor(cfg)
		}()
	default:
		log.Fatalf("Unknown SERVICE_TYPE: %s. Supported values are: registry, executor, all", serviceType)
	}

	wg.Wait()
}

func startRegistry(cfg *config.Config) {
	log.Println("Registry service is starting...")
	registry.Start(cfg)
}

func startExecutor(cfg *config.Config) {
	log.Println("Executor service is starting...")
	// TODO: Initialize and start Executor (Online HTTP server or Batch mode)
}
