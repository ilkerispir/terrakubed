package core

import (
	"strings"
	"testing"

	"github.com/ilkerispir/terrakubed/internal/config"
)

// --- stripScheme ---

func TestStripScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com", "example.com"},
		{"http://example.com", "example.com"},
		{"example.com", "example.com"},
		{"https://api.terrakube.io", "api.terrakube.io"},
		{"http://localhost:8080", "localhost"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripScheme(tt.input)
			if got != tt.want {
				t.Errorf("stripScheme(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- generateAwsBackend ---

func TestGenerateAwsBackend(t *testing.T) {
	p := &JobProcessor{
		Config: &config.Config{
			AwsBucketName: "my-tf-bucket",
			AwsRegion:     "ap-southeast-2",
		},
	}
	stateKey := "tfstate/org-123/ws-456/terraform.tfstate"
	got := p.generateAwsBackend(stateKey)

	assertContains(t, got, `backend "s3"`)
	assertContains(t, got, `bucket = "my-tf-bucket"`)
	assertContains(t, got, `region = "ap-southeast-2"`)
	assertContains(t, got, `key    = "tfstate/org-123/ws-456/terraform.tfstate"`)
}

func TestGenerateAwsBackend_WithStaticCredentials(t *testing.T) {
	p := &JobProcessor{
		Config: &config.Config{
			AwsBucketName: "my-bucket",
			AwsRegion:     "us-east-1",
			AwsAccessKey:  "AKIAIOSFODNN7",
			AwsSecretKey:  "wJalrXUtnFEMI",
		},
	}
	got := p.generateAwsBackend("key")
	assertContains(t, got, `access_key = "AKIAIOSFODNN7"`)
	assertContains(t, got, `secret_key = "wJalrXUtnFEMI"`)
}

func TestGenerateAwsBackend_WithEndpoint(t *testing.T) {
	p := &JobProcessor{
		Config: &config.Config{
			AwsBucketName: "my-bucket",
			AwsRegion:     "us-east-1",
			AwsEndpoint:   "http://minio:9000",
		},
	}
	got := p.generateAwsBackend("key")
	assertContains(t, got, `endpoint`)
	assertContains(t, got, `force_path_style`)
	assertContains(t, got, `skip_credentials_validation`)
}

func TestGenerateAwsBackend_WithoutCredentials(t *testing.T) {
	// When no static creds are set (IRSA / pod identity), access_key must be omitted
	p := &JobProcessor{
		Config: &config.Config{
			AwsBucketName: "my-bucket",
			AwsRegion:     "us-east-1",
		},
	}
	got := p.generateAwsBackend("key")
	if strings.Contains(got, "access_key") {
		t.Errorf("expected no access_key when AwsAccessKey is empty, got: %s", got)
	}
}

// --- generateAzureBackend ---

func TestGenerateAzureBackend(t *testing.T) {
	p := &JobProcessor{
		Config: &config.Config{
			AzureStorageAccountName:   "mystorageaccount",
			AzureStorageContainerName: "tfstate",
		},
	}
	got := p.generateAzureBackend("org-123", "ws-456")

	assertContains(t, got, `backend "azurerm"`)
	assertContains(t, got, `storage_account_name = "mystorageaccount"`)
	assertContains(t, got, `container_name       = "tfstate"`)
	assertContains(t, got, `org-123/ws-456/terraform.tfstate`)
}

// --- generateGcpBackend ---

func TestGenerateGcpBackend(t *testing.T) {
	p := &JobProcessor{
		Config: &config.Config{
			GcpStorageBucketName: "my-gcp-bucket",
		},
	}
	got := p.generateGcpBackend("org-123", "ws-456")

	assertContains(t, got, `backend "gcs"`)
	assertContains(t, got, `bucket = "my-gcp-bucket"`)
	assertContains(t, got, `prefix = "tfstate/org-123/ws-456"`)
}

// --- helpers ---

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q\ngot:\n%s", substr, s)
	}
}
