package handler

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/ilkerispir/terrakubed/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TerraformStateHandler handles /tfstate/v1/ endpoints.
type TerraformStateHandler struct {
	pool     *pgxpool.Pool
	hostname string
	storage  storage.StorageService
}

// NewTerraformStateHandler creates a new handler.
func NewTerraformStateHandler(pool *pgxpool.Pool, hostname string, storage storage.StorageService) *TerraformStateHandler {
	return &TerraformStateHandler{pool: pool, hostname: hostname, storage: storage}
}

// ServeHTTP routes /tfstate/v1/ requests.
func (h *TerraformStateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tfstate/v1/")

	// PUT /tfstate/v1/archive/{archiveId}/terraform.tfstate
	if r.Method == http.MethodPut && strings.HasPrefix(path, "archive/") {
		h.uploadHostedState(w, r, path)
		return
	}

	// GET /tfstate/v1/organization/{orgId}/workspace/{wsId}/state/terraform.tfstate
	// GET /tfstate/v1/organization/{orgId}/workspace/{wsId}/state/{filename}.json
	if r.Method == http.MethodGet && strings.HasPrefix(path, "organization/") {
		h.getState(w, r, path)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

func (h *TerraformStateHandler) getState(w http.ResponseWriter, r *http.Request, path string) {
	// Parse: organization/{orgId}/workspace/{wsId}/state/{filename}
	parts := strings.Split(path, "/")
	if len(parts) < 6 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	orgID := parts[1]
	wsID := parts[3]
	stateFile := parts[5]

	log.Printf("Get state: org=%s ws=%s file=%s", orgID, wsID, stateFile)

	// Read from storage backend
	storagePath := fmt.Sprintf("tfstate/%s/%s/%s", orgID, wsID, stateFile)
	reader, err := h.storage.DownloadFile(storagePath)
	if err != nil {
		log.Printf("Error reading state: %v", err)
		http.Error(w, "State not found", http.StatusNotFound)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

func (h *TerraformStateHandler) uploadHostedState(w http.ResponseWriter, r *http.Request, path string) {
	// Parse: archive/{archiveId}/terraform.tfstate
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	archiveID := parts[1]
	log.Printf("Upload hosted state for archive: %s", archiveID)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Look up archive to find the workspace/org context
	var historyID, orgID, wsID string
	err = h.pool.QueryRow(r.Context(), `
		SELECT a.history_id, o.id, w.id
		FROM temp_archive a
		JOIN history h ON a.history_id = h.id
		JOIN workspace w ON h.workspace_id = w.id
		JOIN organization o ON w.organization_id = o.id
		WHERE a.id = $1
	`, archiveID).Scan(&historyID, &orgID, &wsID)
	if err != nil {
		log.Printf("Archive %s not found: %v", archiveID, err)
		http.Error(w, "Archive not found", http.StatusForbidden)
		return
	}

	// Upload to storage backend
	storagePath := fmt.Sprintf("tfstate/%s/%s/%s.tfstate", orgID, wsID, historyID)
	if err := h.storage.UploadFile(storagePath, bytes.NewReader(body)); err != nil {
		log.Printf("Error uploading state to storage: %v", err)
		http.Error(w, "Failed to upload state", http.StatusInternalServerError)
		return
	}
	log.Printf("Upload state: org=%s ws=%s history=%s (%d bytes)", orgID, wsID, historyID, len(body))

	// Update history with output URL and MD5
	stateHash := fmt.Sprintf("%x", md5.Sum(body))
	outputURL := fmt.Sprintf("https://%s/tfstate/v1/organization/%s/workspace/%s/state/%s.json",
		h.hostname, orgID, wsID, historyID)

	_, err = h.pool.Exec(r.Context(),
		"UPDATE history SET output = $1, md5 = $2 WHERE id = $3",
		outputURL, stateHash, historyID)
	if err != nil {
		log.Printf("Error updating history: %v", err)
	}

	// Delete the archive entry
	_, _ = h.pool.Exec(r.Context(), "DELETE FROM temp_archive WHERE id = $1", archiveID)

	w.WriteHeader(http.StatusCreated)
}

// ──────────────────────────────────────────────────
// TFE Remote State API (/remote/tfe/v2/)
// ──────────────────────────────────────────────────

// RemoteTFEHandler handles TFE-compatible API endpoints.
type RemoteTFEHandler struct {
	pool     *pgxpool.Pool
	hostname string
}

// NewRemoteTFEHandler creates a new handler.
func NewRemoteTFEHandler(pool *pgxpool.Pool, hostname string) *RemoteTFEHandler {
	return &RemoteTFEHandler{pool: pool, hostname: hostname}
}

// ServeHTTP routes /remote/tfe/v2/ requests.
func (h *RemoteTFEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/remote/tfe/v2/")
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case path == "ping":
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	case strings.HasPrefix(path, "workspaces"):
		h.handleWorkspaces(w, r, path)

	case strings.HasPrefix(path, "state-versions"):
		h.handleStateVersions(w, r, path)

	case strings.HasPrefix(path, "runs"):
		h.handleRuns(w, r, path)

	case strings.HasPrefix(path, "plans"):
		h.handlePlans(w, r, path)

	case strings.HasPrefix(path, "configuration-versions"):
		h.handleConfigVersions(w, r, path)

	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *RemoteTFEHandler) handleWorkspaces(w http.ResponseWriter, r *http.Request, path string) {
	// GET /remote/tfe/v2/workspaces?search[name]=xxx
	// This is used by terraform CLI to find workspace by name
	parts := strings.Split(path, "/")
	if r.Method == http.MethodGet && len(parts) == 1 {
		name := r.URL.Query().Get("search[name]")
		if name == "" {
			http.Error(w, "search[name] required", http.StatusBadRequest)
			return
		}

		var wsID, orgID string
		var locked bool
		var terraformVersion *string
		err := h.pool.QueryRow(r.Context(),
			"SELECT w.id, w.organization_id, w.locked, w.terraform_version FROM workspace w WHERE w.name = $1 AND w.deleted = false LIMIT 1",
			name,
		).Scan(&wsID, &orgID, &locked, &terraformVersion)
		if err != nil {
			http.Error(w, "Workspace not found", http.StatusNotFound)
			return
		}

		tfVer := ""
		if terraformVersion != nil {
			tfVer = *terraformVersion
		}

		doc := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   wsID,
					"type": "workspaces",
					"attributes": map[string]interface{}{
						"name":              name,
						"locked":            locked,
						"terraform-version": tfVer,
						"permissions": map[string]bool{
							"can-queue-run":  true,
							"can-lock":       true,
							"can-unlock":     true,
							"can-read-state": true,
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(doc)
		return
	}
	http.Error(w, "Not found", http.StatusNotFound)
}

func (h *RemoteTFEHandler) handleStateVersions(w http.ResponseWriter, r *http.Request, path string) {
	// POST /remote/tfe/v2/state-versions — create a new state version
	if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		log.Printf("Create state version: %d bytes", len(body))

		// Parse the incoming state version request
		var req struct {
			Data struct {
				Type       string `json:"type"`
				Attributes struct {
					Serial  int    `json:"serial"`
					MD5     string `json:"md5"`
					Lineage string `json:"lineage"`
					State   string `json:"state"`
				} `json:"attributes"`
				Relationships struct {
					Workspace struct {
						Data struct {
							ID string `json:"id"`
						} `json:"data"`
					} `json:"workspace"`
				} `json:"relationships"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Create history entry and archive for state upload
		historyID := uuid.New()
		archiveID := uuid.New()

		_, err = h.pool.Exec(r.Context(), `
			INSERT INTO history (id, workspace_id, serial, md5, lineage, job_reference, output)
			VALUES ($1, $2, $3, $4, $5, '', '')
		`, historyID, req.Data.Relationships.Workspace.Data.ID,
			req.Data.Attributes.Serial, req.Data.Attributes.MD5, req.Data.Attributes.Lineage)
		if err != nil {
			log.Printf("Error creating history: %v", err)
			http.Error(w, "Failed to create state version", http.StatusInternalServerError)
			return
		}

		_, err = h.pool.Exec(r.Context(), `
			INSERT INTO temp_archive (id, type, history_id) VALUES ($1, 'state', $2)
		`, archiveID, historyID)
		if err != nil {
			log.Printf("Error creating archive: %v", err)
		}

		uploadURL := fmt.Sprintf("https://%s/tfstate/v1/archive/%s/terraform.tfstate", h.hostname, archiveID)

		doc := map[string]interface{}{
			"data": map[string]interface{}{
				"id":   historyID.String(),
				"type": "state-versions",
				"attributes": map[string]interface{}{
					"upload-url":              uploadURL,
					"hosted-state-upload-url": uploadURL,
					"serial":                  req.Data.Attributes.Serial,
				},
			},
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(doc)
		return
	}
	http.Error(w, "Not found", http.StatusNotFound)
}

func (h *RemoteTFEHandler) handleRuns(w http.ResponseWriter, r *http.Request, path string) {
	log.Printf("TFE runs endpoint: %s %s", r.Method, path)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
}

func (h *RemoteTFEHandler) handlePlans(w http.ResponseWriter, r *http.Request, path string) {
	log.Printf("TFE plans endpoint: %s %s", r.Method, path)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
}

func (h *RemoteTFEHandler) handleConfigVersions(w http.ResponseWriter, r *http.Request, path string) {
	log.Printf("TFE config-versions endpoint: %s %s", r.Method, path)
	// PUT for uploading config — just accept it
	if r.Method == http.MethodPut {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		log.Printf("Config version upload: %d bytes", len(body))
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
}

// ──────────────────────────────────────────────────
// .well-known/terraform.json
// ──────────────────────────────────────────────────

// WellKnownHandler serves /.well-known/terraform.json.
type WellKnownHandler struct {
	hostname string
}

// NewWellKnownHandler creates a new handler.
func NewWellKnownHandler(hostname string) *WellKnownHandler {
	return &WellKnownHandler{hostname: hostname}
}

func (h *WellKnownHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	doc := map[string]interface{}{
		"modules.v1":   fmt.Sprintf("https://%s/registry/v1/modules/", h.hostname),
		"providers.v1": fmt.Sprintf("https://%s/registry/v1/providers/", h.hostname),
		"tfe.v2":       fmt.Sprintf("https://%s/remote/tfe/v2/", h.hostname),
		"tfe.v2.1":     fmt.Sprintf("https://%s/remote/tfe/v2/", h.hostname),
		"motd.v1":      fmt.Sprintf("https://%s/remote/tfe/v2/motd", h.hostname),
	}
	json.NewEncoder(w).Encode(doc)
}
