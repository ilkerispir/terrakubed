package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/ilkerispir/terrakubed/internal/api/repository"
)

// LogsHandler handles the /logs endpoint for Redis log streaming.
type LogsHandler struct {
	repo *repository.GenericRepository
	// redisClient will be injected later
}

// NewLogsHandler creates a new LogsHandler.
func NewLogsHandler(repo *repository.GenericRepository) *LogsHandler {
	return &LogsHandler{repo: repo}
}

// SetupConsumerGroups handles POST /logs/{jobId}/setup-consumer-groups
func (h *LogsHandler) SetupConsumerGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// TODO: Create Redis consumer groups for the job
	log.Printf("Setup consumer groups called")
	w.WriteHeader(http.StatusOK)
}

// AppendLogs handles POST /logs
func (h *LogsHandler) AppendLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Data []struct {
			JobID      interface{} `json:"jobId"`
			StepID     string      `json:"stepId"`
			LineNumber string      `json:"lineNumber"`
			Output     string      `json:"output"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// TODO: Write to Redis stream
	log.Printf("Append logs: %d entries", len(req.Data))
	w.WriteHeader(http.StatusOK)
}

// TerraformOutputHandler serves /tfoutput/v1 — returns job step output.
type TerraformOutputHandler struct {
	repo *repository.GenericRepository
	// storageService and streamingService will be injected later
}

// NewTerraformOutputHandler creates a new TerraformOutputHandler.
func NewTerraformOutputHandler(repo *repository.GenericRepository) *TerraformOutputHandler {
	return &TerraformOutputHandler{repo: repo}
}

// GetOutput handles GET /tfoutput/v1/organization/{orgId}/job/{jobId}/step/{stepId}
func (h *TerraformOutputHandler) GetOutput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// TODO: Read from Redis stream first, fallback to S3
	log.Printf("Get output called: %s", r.URL.Path)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
}

// ContextHandler serves /context/v1 — provides execution context for jobs.
type ContextHandler struct {
	repo *repository.GenericRepository
}

// NewContextHandler creates a new ContextHandler.
func NewContextHandler(repo *repository.GenericRepository) *ContextHandler {
	return &ContextHandler{repo: repo}
}

// GetContext handles GET /context/v1/{jobId}
func (h *ContextHandler) GetContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// TODO: Build execution context from DB (workspace vars, global vars, etc.)
	log.Printf("Get context called: %s", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}
