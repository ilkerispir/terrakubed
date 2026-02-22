package registry

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/ilkerispir/terrakubed/internal/client"
	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/storage"
)

func Start(cfg *config.Config) {

	r := gin.Default()

	// CORS Setup
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization", "X-Terraform-Get"},
		ExposeHeaders:    []string{"Content-Length", "X-Terraform-Get", "Content-Disposition"},
		AllowCredentials: true,
	}))

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "UP",
		})
	})

	// Actuator health endpoints for backward compatibility with Spring Boot probes
	actuatorHealth := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "UP",
		})
	}
	r.GET("/actuator/health", actuatorHealth)
	r.GET("/actuator/health/liveness", actuatorHealth)
	r.GET("/actuator/health/readiness", actuatorHealth)

	// Terraform Registry Service Discovery
	r.GET("/.well-known/terraform.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"modules.v1":   "/terraform/modules/v1/",
			"providers.v1": "/terraform/providers/v1/",
		})
	})

	apiClient := client.NewClient(cfg.AzBuilderApiUrl, cfg.InternalSecret)

	// Initialize Storage Service
	var storageService storage.StorageService
	var err error

	switch cfg.RegistryStorageType {
	case "AWS", "AwsStorageImpl":
		storageService, err = storage.NewAWSStorageService(
			context.TODO(), // TODO: Use proper context
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
		log.Fatalf("Unknown RegistryStorageType: %s. Supported values: AWS, AZURE, GCP", cfg.RegistryStorageType)
	}

	if err != nil {
		log.Fatalf("Failed to initialize storage service (%s): %v", cfg.RegistryStorageType, err)
	}

	// List Module Versions
	r.GET("/terraform/modules/v1/:org/:name/:provider/versions", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")

		versions, err := apiClient.GetModuleVersions(org, name, provider)
		if err != nil {
			log.Printf("Error fetching versions: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch versions"})
			return
		}

		var versionDTOs []gin.H
		for _, v := range versions {
			versionDTOs = append(versionDTOs, gin.H{"version": v})
		}

		c.JSON(http.StatusOK, gin.H{
			"modules": []gin.H{
				{
					"versions": versionDTOs,
				},
			},
		})
	})

	// Download Module Version
	r.GET("/terraform/modules/v1/:org/:name/:provider/:version/download", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")
		version := c.Param("version")

		// Get Module Details for Source/VCS info
		moduleDetails, orgId, err := apiClient.GetModule(org, name, provider)
		if err != nil {
			log.Printf("Error fetching module details: %v", err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
			return
		}

		// Prepare args for SearchModule
		source := moduleDetails.Source
		folder := moduleDetails.Folder
		tagPrefix := moduleDetails.TagPrefix
		vcsType := "PUBLIC"
		accessToken := ""

		if moduleDetails.Vcs != nil && len(moduleDetails.Vcs.Edges) > 0 {
			vcsNode := moduleDetails.Vcs.Edges[0].Node
			vcsType = vcsNode.VcsType

			// To get the accessToken, we must call the REST API: /api/v1/organization/{orgId}/vcs/{vcsId}
			// For now, if we have an apiClient, we can implement GetVcsToken to fetch it.
			token, err := apiClient.GetVcsToken(orgId, vcsNode.ID)
			if err == nil {
				accessToken = token
			} else {
				log.Printf("Warning: Failed to fetch VCS token for VCS ID %s: %v", vcsNode.ID, err)
			}
		} else if moduleDetails.Ssh != nil && len(moduleDetails.Ssh.Edges) > 0 {
			sshNode := moduleDetails.Ssh.Edges[0].Node
			vcsType = "SSH~" + sshNode.SshType
			accessToken = sshNode.PrivateKey
		}

		path, err := storageService.SearchModule(org, name, provider, version, source, vcsType, accessToken, tagPrefix, folder)
		if err != nil {
			log.Printf("Error searching/processing module: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process module download"})
			return
		}

		c.Header("X-Terraform-Get", path)
		c.Status(http.StatusNoContent)
	})

	// Provide README URL
	// Same as module download, but the path should be returned so the UI can download the README from the zip later, or maybe just point to the same zip?
	// Terrakube UI usually expects X-Terraform-Get or it just tries to download it if it's a 204/200 OK.
	r.GET("/terraform/readme/v1/:org/:name/:provider/:version/download", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")
		version := c.Param("version")

		// The module search path logic gives the zip URL. The UI knows how to extract the README from the zip or we can just point it to the zip.
		path := fmt.Sprintf("%s/terraform/modules/v1/download/%s/%s/%s/%s/module.zip", cfg.AzBuilderRegistry, org, name, provider, version)

		c.Header("X-Terraform-Get", path)
		c.Status(http.StatusNoContent)
	})

	// Download Module Zip (Actual File)
	r.GET("/terraform/modules/v1/download/:org/:name/:provider/:version/module.zip", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")
		version := c.Param("version")

		reader, err := storageService.DownloadModule(org, name, provider, version)
		if err != nil {
			log.Printf("Error downloading module zip: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to download module zip"})
			return
		}
		defer reader.Close()

		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s-%s-%s.zip\"", org, name, provider, version))

		extraHeaders := map[string]string{
			"X-Terraform-Get": "", // Clear this if it was set
		}

		c.DataFromReader(http.StatusOK, -1, "application/zip", reader, extraHeaders)
	})

	// Terraform Provider Registry Service Discovery
	// Providers endpoints
	r.GET("/terraform/providers/v1/:org/:provider/versions", func(c *gin.Context) {
		org := c.Param("org")
		provider := c.Param("provider")

		versions, err := apiClient.GetProviderVersions(org, provider)
		if err != nil {
			log.Printf("Error fetching provider versions: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch provider versions"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"versions": versions,
		})
	})

	r.GET("/terraform/providers/v1/:org/:provider/:version/download/:os/:arch", func(c *gin.Context) {
		org := c.Param("org")
		provider := c.Param("provider")
		version := c.Param("version")
		os := c.Param("os")
		arch := c.Param("arch")

		fileData, err := apiClient.GetProviderFile(org, provider, version, os, arch)
		if err != nil {
			log.Printf("Error fetching provider file info: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch provider file info"})
			return
		}

		c.JSON(http.StatusOK, fileData)
	})

	log.Printf("Starting Registry Service on port %s", cfg.Port)
	if err := r.Run(fmt.Sprintf(":%s", cfg.Port)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
