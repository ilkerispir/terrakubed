package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ilkerispir/terrakubed/internal/model"
)

type Config struct {
	// Mixed Config
	Port                      string
	AzBuilderRegistry         string
	AzBuilderApiUrl           string
	RegistryStorageType       string
	AwsBucketName             string
	AwsRegion                 string
	AwsAccessKey              string
	AwsSecretKey              string
	AwsEndpoint               string
	AwsEnableRoleAuth         bool
	PatSecret                 string
	InternalSecret            string
	AzureStorageAccountName   string
	AzureStorageAccountKey    string
	AzureStorageContainerName string
	GcpStorageProjectId       string
	GcpStorageBucketName      string
	GcpStorageCredentials     string

	// Registry Auth
	AuthValidationType string // LOCAL or DEX
	IssuerUri          string
	AppClientId        string
	TerrakubeUiURL     string

	// Executor Specific
	Mode                    string
	EphemeralJobData        *model.TerraformJob
	TerrakubeRegistryDomain string
	StorageType             string
}

func getEnvWithFallback(primary, fallback string) string {
	val := os.Getenv(primary)
	if val == "" {
		return os.Getenv(fallback)
	}
	return val
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// getEnvChain tries multiple environment variable names in order, returning the first non-empty value.
func getEnvChain(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

func getStorageType() string {
	st := os.Getenv("STORAGE_TYPE")
	if st != "" {
		return st
	}
	tst := os.Getenv("TerraformStateType")
	switch tst {
	case "AwsTerraformStateImpl":
		return "AWS"
	case "AzureTerraformStateImpl":
		return "AZURE"
	case "GcpTerraformStateImpl":
		return "GCP"
	case "LocalTerraformStateImpl", "":
		return "LOCAL"
	}
	return "LOCAL"
}

// getExecutorMode determines if executor runs in BATCH (ephemeral) or ONLINE mode.
// Java API sets ExecutorFlagBatch=true and EPHEMERAL_JOB_DATA on ephemeral K8s Jobs.
func getExecutorMode() string {
	if mode := os.Getenv("EXECUTOR_MODE"); mode != "" {
		return mode
	}
	// Java API sets EphemeralFlagBatch=true for ephemeral executors
	if os.Getenv("EphemeralFlagBatch") == "true" || os.Getenv("ExecutorFlagBatch") == "true" {
		return "BATCH"
	}
	// Auto-detect: if EphemeralJobData is present, run in batch mode
	if os.Getenv("EphemeralJobData") != "" || os.Getenv("EPHEMERAL_JOB_DATA") != "" {
		return "BATCH"
	}
	return ""
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		// Registry
		Port:                      getEnv("PORT", "8075"),
		AzBuilderRegistry:         getEnv("AzBuilderRegistry", "http://localhost:8075"),
		AzBuilderApiUrl:           getEnv("AzBuilderApiUrl", "http://localhost:8081"),
		RegistryStorageType:       getEnv("RegistryStorageType", "AWS"),
		AwsBucketName:             getEnvChain("AwsStorageBucketName", "AWS_BUCKET_NAME", "AwsTerraformStateBucketName", "AwsTerraformOutputBucketName"),
		AwsRegion:                 getEnvChain("AwsStorageRegion", "AWS_REGION", "AwsTerraformStateRegion", "AwsTerraformOutputRegion"),
		AwsAccessKey:              getEnvChain("AwsStorageAccessKey", "AWS_ACCESS_KEY_ID", "AwsTerraformStateAccessKey"),
		AwsSecretKey:              getEnvChain("AwsStorageSecretKey", "AWS_SECRET_ACCESS_KEY", "AwsTerraformStateSecretKey"),
		AwsEndpoint:               getEnv("AwsEndpoint", ""),
		AwsEnableRoleAuth:         getEnv("AwsEnableRoleAuth", "false") == "true",
		PatSecret:                 getEnv("PatSecret", ""),
		InternalSecret:            getEnv("InternalSecret", ""),
		AzureStorageAccountName:   getEnv("AzureStorageAccountName", ""),
		AzureStorageAccountKey:    getEnv("AzureStorageAccountKey", ""),
		AzureStorageContainerName: getEnv("AzureStorageContainerName", ""),
		GcpStorageProjectId:       getEnv("GcpStorageProjectId", ""),
		GcpStorageBucketName:      getEnv("GcpStorageBucketName", ""),
		GcpStorageCredentials:     getEnv("GcpStorageCredentials", ""),

		// Registry Auth
		AuthValidationType: getEnvWithFallback("AuthenticationValidationTypeRegistry", "AUTH_VALIDATION_TYPE"),
		IssuerUri:          getEnvWithFallback("DexIssuerUri", "APP_ISSUER_URI"),
		AppClientId:        getEnv("AppClientId", ""),
		TerrakubeUiURL:     getEnvWithFallback("TerrakubeUiURL", "TERRAKUBE_UI_URL"),

		// Executor
		Mode:                    getExecutorMode(),
		TerrakubeRegistryDomain: getEnvWithFallback("TERRAKUBE_REGISTRY_DOMAIN", "TerrakubeRegistryDomain"),
		StorageType:             getStorageType(),
	}

	// Override API / Secret if provided by executor envs
	if api := getEnvWithFallback("TERRAKUBE_API_URL", "TerrakubeApiUrl"); api != "" {
		cfg.AzBuilderApiUrl = api
	}
	if secret := getEnvWithFallback("TERRAKUBE_INTERNAL_SECRET", "InternalSecret"); secret != "" {
		cfg.InternalSecret = secret
	}

	if cfg.Mode == "BATCH" {
		jobData := getEnvChain("EphemeralJobData", "EPHEMERAL_JOB_DATA")
		if jobData == "" {
			return nil, fmt.Errorf("BATCH mode but EphemeralJobData/EPHEMERAL_JOB_DATA is empty")
		}

		decodedData, err := base64.StdEncoding.DecodeString(jobData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode EPHEMERAL_JOB_DATA: %v", err)
		}

		var job model.TerraformJob
		if err := json.Unmarshal(decodedData, &job); err != nil {
			return nil, fmt.Errorf("failed to unmarshal EPHEMERAL_JOB_DATA: %v", err)
		}
		cfg.EphemeralJobData = &job
	}

	return cfg, nil
}
