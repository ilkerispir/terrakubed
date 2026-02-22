package registry

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/ilkerispir/terrakubed/internal/auth"
	"github.com/ilkerispir/terrakubed/internal/client"
	"github.com/ilkerispir/terrakubed/internal/config"
	"github.com/ilkerispir/terrakubed/internal/storage"
)

// cacheEntry stores a cached value with an expiration time.
type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// simpleCache is a thread-safe in-memory cache with TTL.
type simpleCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

func newCache(ttl time.Duration) *simpleCache {
	return &simpleCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

func (c *simpleCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func (c *simpleCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// jwtAuthMiddleware validates JWT tokens for protected endpoints.
func jwtAuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip auth for LOCAL mode
		if cfg.AuthValidationType == "" || cfg.AuthValidationType == "LOCAL" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Bearer token required"})
			return
		}

		_, err := auth.ValidateTokenWithIssuer(tokenString, cfg.InternalSecret, cfg.PatSecret, cfg.IssuerUri)
		if err != nil {
			log.Printf("Token validation failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token: " + err.Error()})
			return
		}

		c.Next()
	}
}

func Start(cfg *config.Config) {

	r := gin.Default()

	// CORS Setup
	corsOrigins := []string{"*"}
	if cfg.TerrakubeUiURL != "" {
		corsOrigins = strings.Split(cfg.TerrakubeUiURL, ",")
	}

	r.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
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

	actuatorHealth := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "UP",
		})
	}
	r.GET("/actuator/health", actuatorHealth)
	r.GET("/actuator/health/liveness", actuatorHealth)
	r.GET("/actuator/health/readiness", actuatorHealth)

	// Terraform Registry Service Discovery (with login.v1)
	r.GET("/.well-known/terraform.json", func(c *gin.Context) {
		response := gin.H{
			"modules.v1":   "/terraform/modules/v1/",
			"providers.v1": "/terraform/providers/v1/",
		}

		if cfg.AppClientId != "" && cfg.IssuerUri != "" {
			response["login.v1"] = gin.H{
				"client":      cfg.AppClientId,
				"grant_types": []string{"authz_code", "openid", "profile", "email", "offline_access", "groups"},
				"authz":       cfg.IssuerUri + "/auth?scope=openid+profile+email+offline_access+groups",
				"token":       cfg.IssuerUri + "/token",
				"ports":       []int{10000, 10001},
			}
		}

		c.JSON(http.StatusOK, response)
	})

	apiClient := client.NewClient(cfg.AzBuilderApiUrl, cfg.InternalSecret)

	// Initialize Storage Service
	var storageService storage.StorageService
	var err error

	switch cfg.RegistryStorageType {
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
		log.Fatalf("Unknown RegistryStorageType: %s. Supported values: AWS, AZURE, GCP", cfg.RegistryStorageType)
	}

	if err != nil {
		log.Fatalf("Failed to initialize storage service (%s): %v", cfg.RegistryStorageType, err)
	}

	// Cache for module lookups (10 minute TTL)
	moduleCache := newCache(10 * time.Minute)

	// Protected endpoints group
	protected := r.Group("/")
	protected.Use(jwtAuthMiddleware(cfg))

	// List Module Versions (protected)
	protected.GET("/terraform/modules/v1/:org/:name/:provider/versions", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")

		cacheKey := fmt.Sprintf("versions-%s-%s-%s", org, name, provider)
		if cached, ok := moduleCache.Get(cacheKey); ok {
			c.JSON(http.StatusOK, cached)
			return
		}

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

		response := gin.H{
			"modules": []gin.H{
				{
					"versions": versionDTOs,
				},
			},
		}

		moduleCache.Set(cacheKey, response)
		c.JSON(http.StatusOK, response)
	})

	// Download Module Version (protected)
	protected.GET("/terraform/modules/v1/:org/:name/:provider/:version/download", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")
		version := c.Param("version")

		cacheKey := fmt.Sprintf("download-%s-%s-%s-%s", org, name, provider, version)
		if cached, ok := moduleCache.Get(cacheKey); ok {
			c.Header("X-Terraform-Get", cached.(string))
			c.Status(http.StatusNoContent)
			return
		}

		moduleDetails, orgId, err := apiClient.GetModule(org, name, provider)
		if err != nil {
			log.Printf("Error fetching module details: %v", err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Module not found"})
			return
		}

		source := moduleDetails.Source
		folder := moduleDetails.Folder
		tagPrefix := moduleDetails.TagPrefix
		vcsType := "PUBLIC"
		accessToken := ""

		if moduleDetails.Vcs != nil && len(moduleDetails.Vcs.Edges) > 0 {
			vcsNode := moduleDetails.Vcs.Edges[0].Node
			vcsType = vcsNode.VcsType

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

		moduleCache.Set(cacheKey, path)
		c.Header("X-Terraform-Get", path)
		c.Status(http.StatusNoContent)
	})

	// README endpoint (protected) - extracts README.md from module zip
	protected.GET("/terraform/readme/v1/:org/:name/:provider/:version/download", func(c *gin.Context) {
		org := c.Param("org")
		name := c.Param("name")
		provider := c.Param("provider")
		version := c.Param("version")

		reader, err := storageService.DownloadModule(org, name, provider, version)
		if err != nil {
			// Module not yet in storage, return URL pointing to module zip
			path := fmt.Sprintf("%s/terraform/modules/v1/download/%s/%s/%s/%s/module.zip", cfg.AzBuilderRegistry, org, name, provider, version)
			c.Header("X-Terraform-Get", path)
			c.Status(http.StatusNoContent)
			return
		}
		defer reader.Close()

		zipData, err := io.ReadAll(reader)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read module zip"})
			return
		}

		readmeContent := extractReadmeFromZip(zipData)
		downloadURL := fmt.Sprintf("%s/terraform/modules/v1/download/%s/%s/%s/%s/module.zip", cfg.AzBuilderRegistry, org, name, provider, version)

		c.JSON(http.StatusOK, gin.H{
			"content": base64.StdEncoding.EncodeToString([]byte(readmeContent)),
			"url":     downloadURL,
		})
	})

	// Download Module Zip (public - Terraform needs this without auth)
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
			"X-Terraform-Get": "",
		}

		c.DataFromReader(http.StatusOK, -1, "application/zip", reader, extraHeaders)
	})

	// Provider endpoints (protected)
	protected.GET("/terraform/providers/v1/:org/:provider/versions", func(c *gin.Context) {
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

	protected.GET("/terraform/providers/v1/:org/:provider/:version/download/:os/:arch", func(c *gin.Context) {
		org := c.Param("org")
		provider := c.Param("provider")
		version := c.Param("version")
		osParam := c.Param("os")
		arch := c.Param("arch")

		fileData, err := apiClient.GetProviderFile(org, provider, version, osParam, arch)
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

// extractReadmeFromZip looks for a README.md file inside a ZIP archive.
func extractReadmeFromZip(data []byte) string {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}

	for _, f := range reader.File {
		name := strings.ToLower(f.Name)
		// Look for README.md at root level or one level deep
		parts := strings.Split(name, "/")
		baseName := parts[len(parts)-1]
		if baseName == "readme.md" && len(parts) <= 2 {
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			defer rc.Close()
			content, err := io.ReadAll(rc)
			if err != nil {
				return ""
			}
			return string(content)
		}
	}

	return ""
}
