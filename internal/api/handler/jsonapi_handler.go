package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/ilkerispir/terrakubed/internal/api/jsonapi"
	"github.com/ilkerispir/terrakubed/internal/api/repository"
)

// JSONAPIHandler handles generic JSON:API requests for all resource types.
type JSONAPIHandler struct {
	repo    *repository.GenericRepository
	configs map[string]*jsonapi.ResourceConfig
}

// NewJSONAPIHandler creates a new handler.
func NewJSONAPIHandler(repo *repository.GenericRepository) *JSONAPIHandler {
	h := &JSONAPIHandler{
		repo:    repo,
		configs: make(map[string]*jsonapi.ResourceConfig),
	}
	h.buildConfigs()
	return h
}

// buildConfigs creates JSON:API ResourceConfigs from registered ResourceMetas.
func (h *JSONAPIHandler) buildConfigs() {
	for typeName, meta := range h.repo.AllMetas() {
		config := &jsonapi.ResourceConfig{
			Type:       typeName,
			PKColumn:   meta.PKColumn,
			ParentRels: make(map[string]jsonapi.ParentRelConfig),
			ChildRels:  make(map[string]jsonapi.ChildRelConfig),
		}

		// Build column mappings
		for _, col := range meta.Columns {
			isPK := col == meta.PKColumn
			isFK := false
			fkRel := ""
			excluded := false

			// Check if this column is a FK
			for relName, parent := range meta.Parents {
				if parent.FKColumn == col {
					isFK = true
					fkRel = relName
					break
				}
			}

			config.Columns = append(config.Columns, jsonapi.ColumnMapping{
				Column:        col,
				JSONAttribute: jsonapi.CamelCase(col),
				IsPK:          isPK,
				IsFK:          isFK,
				FKRelation:    fkRel,
				Excluded:      excluded,
			})
		}

		// Map parent relationships
		for relName, parent := range meta.Parents {
			config.ParentRels[relName] = jsonapi.ParentRelConfig{
				FKColumn:   parent.FKColumn,
				TargetType: parent.ParentType,
			}
		}

		// Map child relationships
		for relName, child := range meta.Children {
			config.ChildRels[relName] = jsonapi.ChildRelConfig{
				TargetType: child.ChildType,
				FKColumn:   child.FKColumn,
			}
		}

		h.configs[typeName] = config
	}
}

// ServeHTTP handles all JSON:API routes under /api/v1/
func (h *JSONAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	// Parse the path: /api/v1/{type}[/{id}[/{relationship}]]
	// or nested: /api/v1/{parentType}/{parentId}/{childRel}[/{childId}]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	path = strings.TrimSuffix(path, "/")
	segments := strings.Split(path, "/")

	if len(segments) == 0 || segments[0] == "" {
		writeError(w, http.StatusNotFound, "Resource type required")
		return
	}

	switch {
	case len(segments) == 1:
		// /api/v1/{type} — List or Create
		h.handleCollection(w, r, segments[0])
	case len(segments) == 2:
		// /api/v1/{type}/{id} — Get, Update, or Delete
		h.handleResource(w, r, segments[0], segments[1])
	case len(segments) == 3:
		// /api/v1/{type}/{id}/{relationship} — List related
		h.handleRelated(w, r, segments[0], segments[1], segments[2])
	case len(segments) == 4 && segments[2] == "relationships":
		// /api/v1/{type}/{id}/relationships/{rel} — Relationship link
		h.handleRelationshipLink(w, r, segments[0], segments[1], segments[3])
	case len(segments) == 4:
		// /api/v1/{parentType}/{parentId}/{childType}/{childId}
		// Nested resource access: resolve child by its ID directly
		h.handleResource(w, r, segments[2], segments[3])
	default:
		writeError(w, http.StatusNotFound, "Invalid path")
	}
}

// ──────────────────────────────────────────────────
// Collection: GET /api/v1/{type} and POST /api/v1/{type}
// ──────────────────────────────────────────────────

func (h *JSONAPIHandler) handleCollection(w http.ResponseWriter, r *http.Request, resourceType string) {
	config, ok := h.configs[resourceType]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown resource type: %s", resourceType))
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listResources(w, r, resourceType, config, repository.ListParams{})
	case http.MethodPost:
		h.createResource(w, r, resourceType, config)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ──────────────────────────────────────────────────
// Single resource: GET/PATCH/DELETE /api/v1/{type}/{id}
// ──────────────────────────────────────────────────

func (h *JSONAPIHandler) handleResource(w http.ResponseWriter, r *http.Request, resourceType, idStr string) {
	config, ok := h.configs[resourceType]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown resource type: %s", resourceType))
		return
	}

	id, err := parseID(idStr, h.repo, resourceType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getResource(w, r, resourceType, config, id)
	case http.MethodPatch:
		h.updateResource(w, r, resourceType, config, id)
	case http.MethodDelete:
		h.deleteResource(w, r, resourceType, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ──────────────────────────────────────────────────
// Related: GET /api/v1/{parentType}/{parentId}/{childRel}
// ──────────────────────────────────────────────────

func (h *JSONAPIHandler) handleRelated(w http.ResponseWriter, r *http.Request, parentType, parentIDStr, relName string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	parentConfig, ok := h.configs[parentType]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown resource type: %s", parentType))
		return
	}

	// Check child relationships
	childRel, ok := parentConfig.ChildRels[relName]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown relationship: %s", relName))
		return
	}

	childConfig, ok := h.configs[childRel.TargetType]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown child type: %s", childRel.TargetType))
		return
	}

	parentID, err := parseID(parentIDStr, h.repo, parentType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := repository.ListParams{
		ParentFK: childRel.FKColumn,
		ParentID: parentID,
	}

	h.listResources(w, r, childRel.TargetType, childConfig, params)
}

// ──────────────────────────────────────────────────
// Relationship link: GET /api/v1/{type}/{id}/relationships/{rel}
// ──────────────────────────────────────────────────

func (h *JSONAPIHandler) handleRelationshipLink(w http.ResponseWriter, r *http.Request, resourceType, idStr, relName string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	config, ok := h.configs[resourceType]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown resource type: %s", resourceType))
		return
	}

	id, err := parseID(idStr, h.repo, resourceType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Get the parent resource to read the FK value
	row, err := h.repo.FindByID(r.Context(), resourceType, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "Resource not found")
		return
	}

	// To-one relationship
	if parentRel, ok := config.ParentRels[relName]; ok {
		fkVal := row[parentRel.FKColumn]
		var data interface{}
		if fkVal != nil {
			data = jsonapi.ResourceIdentifier{
				Type: parentRel.TargetType,
				ID:   fmt.Sprintf("%v", fkVal),
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": data,
		})
		return
	}

	// To-many relationship
	if childRel, ok := config.ChildRels[relName]; ok {
		rows, err := h.repo.List(r.Context(), childRel.TargetType, repository.ListParams{
			ParentFK: childRel.FKColumn,
			ParentID: id,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		childMeta, _ := h.repo.GetMeta(childRel.TargetType)
		var identifiers []jsonapi.ResourceIdentifier
		for _, row := range rows {
			identifiers = append(identifiers, jsonapi.ResourceIdentifier{
				Type: childRel.TargetType,
				ID:   fmt.Sprintf("%v", row[childMeta.PKColumn]),
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": identifiers,
		})
		return
	}

	writeError(w, http.StatusNotFound, fmt.Sprintf("Unknown relationship: %s", relName))
}

// ──────────────────────────────────────────────────
// Core CRUD operations
// ──────────────────────────────────────────────────

func (h *JSONAPIHandler) listResources(w http.ResponseWriter, r *http.Request, resourceType string, config *jsonapi.ResourceConfig, params repository.ListParams) {
	// Parse query params for filtering, sorting, pagination
	q := r.URL.Query()

	// Filters: ?filter[name]=value
	if params.Filters == nil {
		params.Filters = make(map[string]interface{})
	}
	for key, values := range q {
		if strings.HasPrefix(key, "filter[") && strings.HasSuffix(key, "]") {
			filterName := key[7 : len(key)-1]
			// Convert camelCase filter name to snake_case column name
			colName := toSnakeCase(filterName)
			params.Filters[colName] = values[0]
		}
	}

	// Sort: ?sort=name or ?sort=-name
	if sort := q.Get("sort"); sort != "" {
		params.Sort = sort
	}

	// Pagination: ?page[number]=1&page[size]=20
	if sizeStr := q.Get("page[size]"); sizeStr != "" {
		if size, err := strconv.Atoi(sizeStr); err == nil {
			params.PageSize = size
		}
	}
	if numStr := q.Get("page[number]"); numStr != "" {
		if num, err := strconv.Atoi(numStr); err == nil && params.PageSize > 0 {
			params.PageOffset = (num - 1) * params.PageSize
		}
	}

	rows, err := h.repo.List(r.Context(), resourceType, params)
	if err != nil {
		log.Printf("Error listing %s: %v", resourceType, err)
		writeError(w, http.StatusInternalServerError, "Failed to list resources")
		return
	}

	basePath := "/api/v1"
	doc := jsonapi.SerializeList(config, rows, basePath)
	writeJSON(w, http.StatusOK, doc)
}

func (h *JSONAPIHandler) getResource(w http.ResponseWriter, r *http.Request, resourceType string, config *jsonapi.ResourceConfig, id interface{}) {
	row, err := h.repo.FindByID(r.Context(), resourceType, id)
	if err != nil {
		log.Printf("Error getting %s/%v: %v", resourceType, id, err)
		writeError(w, http.StatusInternalServerError, "Failed to get resource")
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "Resource not found")
		return
	}

	basePath := "/api/v1"
	doc := jsonapi.SerializeSingle(config, row, basePath)

	// Handle ?include=rel1,rel2,...
	if includes := r.URL.Query().Get("include"); includes != "" {
		for _, relName := range strings.Split(includes, ",") {
			relName = strings.TrimSpace(relName)
			childRel, ok := config.ChildRels[relName]
			if !ok {
				continue // Skip unknown relationships
			}
			childConfig, ok := h.configs[childRel.TargetType]
			if !ok {
				continue
			}

			childRows, err := h.repo.List(r.Context(), childRel.TargetType, repository.ListParams{
				ParentFK: childRel.FKColumn,
				ParentID: id,
			})
			if err != nil {
				log.Printf("Error loading include %s for %s/%v: %v", relName, resourceType, id, err)
				continue
			}

			for _, childRow := range childRows {
				doc.Included = append(doc.Included, jsonapi.Serialize(childConfig, childRow, basePath))
			}
		}
	}

	writeJSON(w, http.StatusOK, doc)
}

func (h *JSONAPIHandler) createResource(w http.ResponseWriter, r *http.Request, resourceType string, config *jsonapi.ResourceConfig) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	var reqDoc jsonapi.RequestDocument
	if err := json.Unmarshal(body, &reqDoc); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	data := jsonapi.Deserialize(config, reqDoc.Data)

	// Generate UUID for PK if needed
	meta, _ := h.repo.GetMeta(resourceType)
	if meta.PKType == "uuid" {
		if _, ok := data[meta.PKColumn]; !ok {
			data[meta.PKColumn] = uuid.New().String()
		}
	}

	id, err := h.repo.Create(r.Context(), resourceType, data)
	if err != nil {
		log.Printf("Error creating %s: %v", resourceType, err)
		writeError(w, http.StatusInternalServerError, "Failed to create resource")
		return
	}

	// Reload and return
	row, err := h.repo.FindByID(r.Context(), resourceType, id)
	if err != nil || row == nil {
		writeError(w, http.StatusInternalServerError, "Created but failed to reload")
		return
	}

	basePath := "/api/v1"
	doc := jsonapi.SerializeSingle(config, row, basePath)
	writeJSON(w, http.StatusCreated, doc)
}

func (h *JSONAPIHandler) updateResource(w http.ResponseWriter, r *http.Request, resourceType string, config *jsonapi.ResourceConfig, id interface{}) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	var reqDoc jsonapi.RequestDocument
	if err := json.Unmarshal(body, &reqDoc); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	data := jsonapi.Deserialize(config, reqDoc.Data)

	if err := h.repo.Update(r.Context(), resourceType, id, data); err != nil {
		log.Printf("Error updating %s/%v: %v", resourceType, id, err)
		writeError(w, http.StatusInternalServerError, "Failed to update resource")
		return
	}

	// Reload and return
	row, err := h.repo.FindByID(r.Context(), resourceType, id)
	if err != nil || row == nil {
		writeError(w, http.StatusInternalServerError, "Updated but failed to reload")
		return
	}

	basePath := "/api/v1"
	doc := jsonapi.SerializeSingle(config, row, basePath)
	writeJSON(w, http.StatusOK, doc)
}

func (h *JSONAPIHandler) deleteResource(w http.ResponseWriter, r *http.Request, resourceType string, id interface{}) {
	if err := h.repo.Delete(r.Context(), resourceType, id); err != nil {
		log.Printf("Error deleting %s/%v: %v", resourceType, id, err)
		writeError(w, http.StatusInternalServerError, "Failed to delete resource")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ──────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────

func parseID(idStr string, repo *repository.GenericRepository, resourceType string) (interface{}, error) {
	meta, ok := repo.GetMeta(resourceType)
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}

	switch meta.PKType {
	case "uuid":
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID: %s", idStr)
		}
		return id, nil
	case "int":
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid integer ID: %s", idStr)
		}
		return id, nil
	case "string":
		return idStr, nil
	default:
		return idStr, nil
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func writeError(w http.ResponseWriter, statusCode int, detail string) {
	w.WriteHeader(statusCode)
	errDoc := jsonapi.ErrorDocument{
		Errors: []jsonapi.Error{
			{
				Status: strconv.Itoa(statusCode),
				Title:  http.StatusText(statusCode),
				Detail: detail,
			},
		},
	}
	json.NewEncoder(w).Encode(errDoc)
}

func toSnakeCase(s string) string {
	var result strings.Builder
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(c + 32)
		} else {
			result.WriteRune(c)
		}
	}
	return result.String()
}
