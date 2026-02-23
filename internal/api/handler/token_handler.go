package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PatHandler handles /pat/v1 endpoints for Personal Access Token management.
type PatHandler struct {
	pool      *pgxpool.Pool
	patSecret string // base64url-encoded HMAC key
}

// NewPatHandler creates a new handler.
func NewPatHandler(pool *pgxpool.Pool, patSecret string) *PatHandler {
	return &PatHandler{pool: pool, patSecret: patSecret}
}

type patCreateRequest struct {
	Days        int    `json:"days"`
	Description string `json:"description"`
}

type patCreateResponse struct {
	Token string `json:"token"`
}

type patListItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Days        int    `json:"days"`
	CreatedDate string `json:"createdDate,omitempty"`
	CreatedBy   string `json:"createdBy,omitempty"`
}

func (h *PatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		h.listTokens(w, r)
	case http.MethodPost:
		h.createToken(w, r)
	case http.MethodDelete:
		h.deleteToken(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *PatHandler) createToken(w http.ResponseWriter, r *http.Request) {
	var req patCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user info from context (set by auth middleware)
	email := r.Header.Get("X-User-Email")
	name := r.Header.Get("X-User-Name")
	groups := r.Header.Get("X-User-Groups")
	if email == "" {
		email = "unknown"
	}
	if name == "" {
		name = email
	}

	// Insert PAT record
	patID := uuid.New()
	_, err := h.pool.Exec(r.Context(),
		`INSERT INTO pat (id, days, deleted, description, created_by, created_date)
		 VALUES ($1, $2, false, $3, $4, $5)`,
		patID, req.Days, req.Description, email, time.Now(),
	)
	if err != nil {
		log.Printf("Failed to create PAT: %v", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	// Generate JWT
	claims := jwt.MapClaims{
		"iss":            "Terrakube",
		"sub":            fmt.Sprintf("%s (Token)", name),
		"aud":            "Terrakube",
		"jti":            patID.String(),
		"email":          email,
		"email_verified": true,
		"name":           fmt.Sprintf("%s (Token)", name),
		"iat":            time.Now().Unix(),
	}

	// Parse groups
	if groups != "" {
		claims["groups"] = strings.Split(groups, ",")
	}

	// Set expiration if days > 0
	if req.Days > 0 {
		claims["exp"] = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signingKey := []byte(h.patSecret)
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		log.Printf("Failed to sign PAT JWT: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(patCreateResponse{Token: tokenString})
}

func (h *PatHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	email := r.Header.Get("X-User-Email")

	rows, err := h.pool.Query(r.Context(),
		`SELECT id, description, days, created_date, created_by
		 FROM pat WHERE deleted = false AND created_by = $1
		 ORDER BY created_date DESC`, email,
	)
	if err != nil {
		log.Printf("Failed to list PATs: %v", err)
		http.Error(w, "Failed to list tokens", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tokens := make([]patListItem, 0)
	for rows.Next() {
		var p patListItem
		var createdDate time.Time
		if err := rows.Scan(&p.ID, &p.Description, &p.Days, &createdDate, &p.CreatedBy); err != nil {
			continue
		}
		p.CreatedDate = createdDate.Format(time.RFC3339)
		tokens = append(tokens, p)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(tokens)
}

func (h *PatHandler) deleteToken(w http.ResponseWriter, r *http.Request) {
	// Extract token ID from path: /pat/v1/{tokenId}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/pat/v1/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Token ID required", http.StatusBadRequest)
		return
	}
	tokenID := parts[0]

	result, err := h.pool.Exec(r.Context(),
		`UPDATE pat SET deleted = true WHERE id = $1`, tokenID,
	)
	if err != nil {
		log.Printf("Failed to delete PAT: %v", err)
		http.Error(w, "Failed to delete token", http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		http.Error(w, "Token not found", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// ──────────────────────────────────────────────────
// TeamTokenHandler handles /access-token/v1/teams endpoints.
// ──────────────────────────────────────────────────

type TeamTokenHandler struct {
	pool       *pgxpool.Pool
	patSecret  string
	ownerGroup string
}

func NewTeamTokenHandler(pool *pgxpool.Pool, patSecret, ownerGroup string) *TeamTokenHandler {
	return &TeamTokenHandler{pool: pool, patSecret: patSecret, ownerGroup: ownerGroup}
}

func (h *TeamTokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/access-token/v1/teams")

	switch {
	case r.Method == http.MethodOptions:
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodGet && path == "/current-teams":
		h.currentTeams(w, r)

	case r.Method == http.MethodGet && strings.HasPrefix(path, "/permissions/organization/"):
		h.getPermissions(w, r, path)

	case r.Method == http.MethodGet && (path == "" || path == "/"):
		h.listTokens(w, r)

	case r.Method == http.MethodPost && (path == "" || path == "/"):
		h.createToken(w, r)

	case r.Method == http.MethodDelete:
		h.deleteToken(w, r, path)

	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *TeamTokenHandler) currentTeams(w http.ResponseWriter, r *http.Request) {
	// Get user's groups from auth header (set by middleware)
	groupsStr := r.Header.Get("X-User-Groups")
	groups := make([]string, 0)
	if groupsStr != "" {
		for _, g := range strings.Split(groupsStr, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				groups = append(groups, g)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string][]string{"groups": groups})
}

func (h *TeamTokenHandler) getPermissions(w http.ResponseWriter, r *http.Request, path string) {
	// Parse: /permissions/organization/{orgId} or /permissions/organization/{orgId}/workspace/{wsId}
	parts := strings.Split(strings.TrimPrefix(path, "/permissions/organization/"), "/")
	orgID := parts[0]

	groupsStr := r.Header.Get("X-User-Groups")
	groups := strings.Split(groupsStr, ",")

	// Check if user is owner
	isOwner := false
	for _, g := range groups {
		if strings.TrimSpace(g) == h.ownerGroup {
			isOwner = true
			break
		}
	}

	permissions := map[string]bool{
		"manageState":      isOwner,
		"manageWorkspace":  isOwner,
		"manageModule":     isOwner,
		"manageProvider":   isOwner,
		"manageVcs":        isOwner,
		"manageTemplate":   isOwner,
		"manageCollection": isOwner,
		"manageJob":        isOwner,
	}

	if !isOwner {
		// Query team permissions from DB
		h.loadTeamPermissions(r.Context(), orgID, groups, permissions)
	}

	// If workspace ID is provided, also check workspace-level access
	if len(parts) >= 3 && parts[1] == "workspace" {
		wsID := parts[2]
		h.loadWorkspacePermissions(r.Context(), wsID, groups, permissions)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(permissions)
}

func (h *TeamTokenHandler) loadTeamPermissions(ctx context.Context, orgID string, groups []string, permissions map[string]bool) {
	if len(groups) == 0 {
		return
	}

	// Build IN clause for groups
	args := []interface{}{orgID}
	placeholders := make([]string, len(groups))
	for i, g := range groups {
		args = append(args, strings.TrimSpace(g))
		placeholders[i] = fmt.Sprintf("$%d", i+2)
	}

	query := fmt.Sprintf(`SELECT manage_state, manage_workspace, manage_module,
		manage_provider, manage_vcs, manage_template, manage_collection, manage_job
		FROM team WHERE organization_id = $1 AND name IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		log.Printf("Failed to load team permissions: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ms, mw, mm, mp, mv, mt, mc, mj bool
		if err := rows.Scan(&ms, &mw, &mm, &mp, &mv, &mt, &mc, &mj); err != nil {
			continue
		}
		permissions["manageState"] = permissions["manageState"] || ms
		permissions["manageWorkspace"] = permissions["manageWorkspace"] || mw
		permissions["manageModule"] = permissions["manageModule"] || mm
		permissions["manageProvider"] = permissions["manageProvider"] || mp
		permissions["manageVcs"] = permissions["manageVcs"] || mv
		permissions["manageTemplate"] = permissions["manageTemplate"] || mt
		permissions["manageCollection"] = permissions["manageCollection"] || mc
		permissions["manageJob"] = permissions["manageJob"] || mj
	}
}

func (h *TeamTokenHandler) loadWorkspacePermissions(ctx context.Context, wsID string, groups []string, permissions map[string]bool) {
	rows, err := h.pool.Query(ctx,
		`SELECT manage_state, manage_workspace, manage_job FROM access WHERE workspace_id = $1`, wsID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ms, mw, mj bool
		if err := rows.Scan(&ms, &mw, &mj); err != nil {
			continue
		}
		permissions["manageState"] = permissions["manageState"] || ms
		permissions["manageWorkspace"] = permissions["manageWorkspace"] || mw
		permissions["manageJob"] = permissions["manageJob"] || mj
	}
}

type teamTokenCreateRequest struct {
	Group       string `json:"group"`
	Description string `json:"description"`
	Days        int    `json:"days"`
	Hours       int    `json:"hours"`
	Minutes     int    `json:"minutes"`
}

func (h *TeamTokenHandler) createToken(w http.ResponseWriter, r *http.Request) {
	var req teamTokenCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	email := r.Header.Get("X-User-Email")
	name := r.Header.Get("X-User-Name")
	if name == "" {
		name = email
	}

	duration := time.Duration(req.Days)*24*time.Hour +
		time.Duration(req.Hours)*time.Hour +
		time.Duration(req.Minutes)*time.Minute

	claims := jwt.MapClaims{
		"iss":            "Terrakube",
		"sub":            fmt.Sprintf("%s (Team Token)", name),
		"aud":            "Terrakube",
		"jti":            uuid.New().String(),
		"email":          email,
		"email_verified": true,
		"name":           fmt.Sprintf("%s (Team Token)", name),
		"groups":         []string{req.Group},
		"iat":            time.Now().Unix(),
	}

	if duration > 0 {
		claims["exp"] = time.Now().Add(duration).Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(h.patSecret))
	if err != nil {
		log.Printf("Failed to sign team token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"token": tokenString})
}

func (h *TeamTokenHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	// Group table stores team tokens in Java API
	email := r.Header.Get("X-User-Email")

	rows, err := h.pool.Query(r.Context(),
		`SELECT id, description, days, created_date, created_by
		 FROM "group" WHERE deleted = false AND created_by = $1
		 ORDER BY created_date DESC`, email,
	)
	if err != nil {
		// Table might not exist or be named differently — return empty
		log.Printf("Failed to list team tokens: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	tokens := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, desc, createdBy string
		var days int
		var createdDate time.Time
		if err := rows.Scan(&id, &desc, &days, &createdDate, &createdBy); err != nil {
			continue
		}
		tokens = append(tokens, map[string]interface{}{
			"id":          id,
			"description": desc,
			"days":        days,
			"createdDate": createdDate.Format(time.RFC3339),
			"createdBy":   createdBy,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(tokens)
}

func (h *TeamTokenHandler) deleteToken(w http.ResponseWriter, r *http.Request, path string) {
	tokenID := strings.TrimPrefix(path, "/")
	if tokenID == "" {
		http.Error(w, "Token ID required", http.StatusBadRequest)
		return
	}

	result, err := h.pool.Exec(r.Context(),
		`UPDATE "group" SET deleted = true WHERE id = $1`, tokenID,
	)
	if err != nil {
		http.Error(w, "Failed to delete token", http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		http.Error(w, "Token not found", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
