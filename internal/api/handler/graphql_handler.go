package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/api/repository"
)

// GraphQLHandler handles Elide-compatible GraphQL requests at /graphql/api/v1.
// Elide's GraphQL uses a connection/edges/node pattern:
//
//	query { organization { edges { node { id name } } } }
//
// This handler parses the query, extracts the resource type and requested fields,
// then maps to the generic repository.
type GraphQLHandler struct {
	repo *repository.GenericRepository
}

// NewGraphQLHandler creates a new handler.
func NewGraphQLHandler(repo *repository.GenericRepository) *GraphQLHandler {
	return &GraphQLHandler{repo: repo}
}

// GraphQL request/response types
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Errors []gqlError  `json:"errors,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

func (h *GraphQLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeGQLError(w, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var req graphQLRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeGQLError(w, "Invalid JSON")
		return
	}

	log.Printf("GraphQL query: %s", truncate(req.Query, 200))

	result, err := h.executeQuery(r.Context(), req.Query, req.Variables)
	if err != nil {
		log.Printf("GraphQL error: %v", err)
		writeGQLError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(graphQLResponse{Data: result})
}

// executeQuery parses an Elide-style GraphQL query and executes it.
func (h *GraphQLHandler) executeQuery(ctx context.Context, query string, variables map[string]interface{}) (interface{}, error) {
	query = strings.TrimSpace(query)

	// Determine if it's a query or mutation
	if strings.HasPrefix(query, "mutation") {
		return h.executeMutation(ctx, query, variables)
	}

	// Parse the root resource type from the query
	// Pattern: { resourceType { edges { node { field1 field2 } } } }
	// or: { resourceType(ids: ["..."]) { edges { node { field1 field2 } } } }
	rootType, ids, fields, relationships := parseGraphQLQuery(query)
	if rootType == "" {
		return nil, fmt.Errorf("could not parse query")
	}

	log.Printf("GraphQL: type=%s ids=%v fields=%v rels=%v", rootType, ids, fields, relationships)

	// Check if the resource type is registered
	meta, ok := h.repo.GetMeta(rootType)
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", rootType)
	}

	// Build the result
	if len(ids) > 0 {
		// Fetch specific records by ID
		return h.fetchByIDs(ctx, rootType, meta, ids, fields, relationships)
	}

	// List all records
	return h.fetchAll(ctx, rootType, meta, fields, relationships)
}

func (h *GraphQLHandler) fetchByIDs(ctx context.Context, resourceType string, meta *repository.ResourceMeta, ids []string, fields []string, relationships []relInfo) (interface{}, error) {
	nodes := make([]map[string]interface{}, 0)

	for _, id := range ids {
		row, err := h.repo.FindByID(ctx, resourceType, id)
		if err != nil {
			continue
		}
		node := filterFields(row, fields)

		// Resolve relationships
		for _, rel := range relationships {
			relData, err := h.resolveRelationship(ctx, id, meta, rel)
			if err == nil {
				node[rel.name] = relData
			}
		}

		nodes = append(nodes, node)
	}

	return map[string]interface{}{
		resourceType: map[string]interface{}{
			"edges": wrapEdges(nodes),
		},
	}, nil
}

func (h *GraphQLHandler) fetchAll(ctx context.Context, resourceType string, meta *repository.ResourceMeta, fields []string, relationships []relInfo) (interface{}, error) {
	rows, err := h.repo.List(ctx, resourceType, repository.ListParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", resourceType, err)
	}

	nodes := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		node := filterFields(row, fields)

		// Resolve relationships
		id := fmt.Sprintf("%v", row[meta.PKColumn])
		for _, rel := range relationships {
			relData, err := h.resolveRelationship(ctx, id, meta, rel)
			if err == nil {
				node[rel.name] = relData
			}
		}

		nodes = append(nodes, node)
	}

	return map[string]interface{}{
		resourceType: map[string]interface{}{
			"edges": wrapEdges(nodes),
		},
	}, nil
}

func (h *GraphQLHandler) resolveRelationship(ctx context.Context, parentID string, parentMeta *repository.ResourceMeta, rel relInfo) (interface{}, error) {
	// Check if this is a child relationship
	childRel, ok := parentMeta.Children[rel.name]
	if !ok {
		return nil, fmt.Errorf("unknown relationship: %s", rel.name)
	}

	// Query children using the FK column
	params := repository.ListParams{
		ParentFK: childRel.FKColumn,
		ParentID: parentID,
	}
	rows, err := h.repo.List(ctx, childRel.ChildType, params)
	if err != nil {
		return nil, err
	}

	// Get child meta for resolving nested relationships
	childMeta, _ := h.repo.GetMeta(childRel.ChildType)

	nodes := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		node := filterFields(row, rel.fields)

		// Resolve nested sub-relationships (e.g., workspaceTag inside workspace)
		if childMeta != nil {
			childID := fmt.Sprintf("%v", row[childMeta.PKColumn])
			for _, subRel := range rel.rels {
				subData, err := h.resolveRelationship(ctx, childID, childMeta, subRel)
				if err == nil {
					node[subRel.name] = subData
				}
			}
		}

		nodes = append(nodes, node)
	}

	return map[string]interface{}{
		"edges": wrapEdges(nodes),
	}, nil
}

// executeMutation handles create/update/delete mutations.
func (h *GraphQLHandler) executeMutation(ctx context.Context, query string, variables map[string]interface{}) (interface{}, error) {
	// Elide mutations look like:
	// mutation { organization(op: UPSERT, data: {id: "...", name: "..."}) { edges { node { id name } } } }
	// mutation { organization(op: DELETE, ids: ["..."]) { edges { node { id } } } }
	log.Printf("GraphQL mutation: %s", truncate(query, 200))

	rootType, op, data := parseMutation(query, variables)
	if rootType == "" {
		return nil, fmt.Errorf("could not parse mutation")
	}

	_, ok := h.repo.GetMeta(rootType)
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", rootType)
	}

	switch strings.ToUpper(op) {
	case "UPSERT":
		id, _ := data["id"].(string)
		if id != "" {
			// Update
			if err := h.repo.Update(ctx, rootType, id, data); err != nil {
				return nil, err
			}
			row, err := h.repo.FindByID(ctx, rootType, id)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				rootType: map[string]interface{}{
					"edges": wrapEdges([]map[string]interface{}{row}),
				},
			}, nil
		}
		// Create
		newID, err := h.repo.Create(ctx, rootType, data)
		if err != nil {
			return nil, err
		}
		row, err := h.repo.FindByID(ctx, rootType, newID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			rootType: map[string]interface{}{
				"edges": wrapEdges([]map[string]interface{}{row}),
			},
		}, nil

	case "DELETE":
		id, _ := data["id"].(string)
		if id == "" {
			return nil, fmt.Errorf("DELETE requires id")
		}
		if err := h.repo.Delete(ctx, rootType, id); err != nil {
			return nil, err
		}
		return map[string]interface{}{rootType: nil}, nil

	default:
		return nil, fmt.Errorf("unknown operation: %s", op)
	}
}

// ──────────────────────────────────────────────────
// Query parsing helpers — proper brace-matching
// ──────────────────────────────────────────────────

type relInfo struct {
	name   string
	fields []string
	rels   []relInfo // nested relationships
}

var (
	// Matches root type: { organization { ... } }
	rootTypeRe = regexp.MustCompile(`(?:query\s*(?:\w+)?\s*)?[{]\s*(\w+)`)
	// Matches ids parameter: (ids: ["id1", "id2"])
	idsRe = regexp.MustCompile(`\(\s*ids?\s*:\s*\[([^\]]*)\]`)
	// Matches single id filter: (id: "uuid")
	singleIDRe = regexp.MustCompile(`\(\s*ids?\s*:\s*"([^"]+)"`)
)

func parseGraphQLQuery(query string) (rootType string, ids []string, fields []string, relationships []relInfo) {
	// Extract root type
	matches := rootTypeRe.FindStringSubmatch(query)
	if len(matches) < 2 {
		return
	}
	rootType = matches[1]

	// Find content after root type name
	inner := query
	if idx := strings.Index(inner, rootType); idx >= 0 {
		inner = inner[idx+len(rootType):]
	}

	// Extract IDs if present
	if idMatches := idsRe.FindStringSubmatch(inner); len(idMatches) >= 2 {
		for _, id := range strings.Split(idMatches[1], ",") {
			id = strings.Trim(strings.TrimSpace(id), `"'`)
			if id != "" {
				ids = append(ids, id)
			}
		}
	} else if singleMatch := singleIDRe.FindStringSubmatch(inner); len(singleMatch) >= 2 {
		ids = append(ids, singleMatch[1])
	}

	// Find the root node's body using brace matching
	// We need to find: edges { node { BODY } }
	nodeBody := findNodeBody(inner)
	if nodeBody == "" {
		return
	}

	// Parse the node body into fields and relationships
	fields, relationships = parseNodeBody(nodeBody)
	return
}

// findNodeBody finds the content inside the first `node { ... }` with proper brace matching.
func findNodeBody(s string) string {
	// Find "node" keyword followed by "{"
	idx := 0
	for {
		pos := strings.Index(s[idx:], "node")
		if pos < 0 {
			return ""
		}
		pos += idx
		// Make sure it's the word "node" (not part of another word)
		after := pos + 4
		if after < len(s) && (s[after] == ' ' || s[after] == '\t' || s[after] == '\n' || s[after] == '{') {
			// Find the opening brace
			braceStart := strings.Index(s[after:], "{")
			if braceStart < 0 {
				return ""
			}
			braceStart += after
			return extractBraceContent(s, braceStart)
		}
		idx = pos + 4
	}
}

// extractBraceContent extracts content between matching braces starting at position of '{'.
func extractBraceContent(s string, openPos int) string {
	depth := 0
	for i := openPos; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[openPos+1 : i]
			}
		}
	}
	return ""
}

// parseNodeBody parses the content inside a node { ... } block into scalar fields and relationships.
// A relationship is identified by a word followed by optional args and a `{`.
func parseNodeBody(body string) (fields []string, rels []relInfo) {
	body = strings.TrimSpace(body)
	i := 0

	for i < len(body) {
		// Skip whitespace
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}
		if i >= len(body) {
			break
		}

		// Read a word (field or relationship name)
		wordStart := i
		for i < len(body) && isWordChar(body[i]) {
			i++
		}
		if wordStart == i {
			i++ // skip non-word char
			continue
		}
		word := body[wordStart:i]

		// Skip whitespace
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}

		// Skip optional arguments: (sort: "name", filter: ...)
		if i < len(body) && body[i] == '(' {
			depth := 0
			for i < len(body) {
				if body[i] == '(' {
					depth++
				} else if body[i] == ')' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
			// Skip whitespace after args
			for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
				i++
			}
		}

		// Check if this is a relationship (has a { block) or a scalar field
		if i < len(body) && body[i] == '{' {
			// It's a relationship — extract its brace content
			content := extractBraceContent(body, i)
			// Advance past the closing brace
			depth := 0
			for i < len(body) {
				if body[i] == '{' {
					depth++
				} else if body[i] == '}' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}

			// Parse the relationship's inner content
			// Look for edges { node { FIELDS } } pattern inside
			relNodeBody := findNodeBody(content)
			if relNodeBody != "" {
				relFields, subRels := parseNodeBody(relNodeBody)
				rels = append(rels, relInfo{name: word, fields: relFields, rels: subRels})
			}
		} else {
			// It's a scalar field
			fields = append(fields, word)
		}
	}
	return
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func parseMutation(query string, variables map[string]interface{}) (rootType string, op string, data map[string]interface{}) {
	data = make(map[string]interface{})

	// Extract root type after "mutation"
	matches := regexp.MustCompile(`mutation\s*(?:\w+)?\s*[{]\s*(\w+)`).FindStringSubmatch(query)
	if len(matches) < 2 {
		return
	}
	rootType = matches[1]

	// Extract operation
	opMatch := regexp.MustCompile(`op\s*:\s*(\w+)`).FindStringSubmatch(query)
	if len(opMatch) >= 2 {
		op = opMatch[1]
	}

	// For now, extract data from variables if present
	if variables != nil {
		if d, ok := variables["data"]; ok {
			if dm, ok := d.(map[string]interface{}); ok {
				data = dm
			}
		}
	}

	// Try to extract inline data: data: { key: "value" ... }
	dataMatch := regexp.MustCompile(`data\s*:\s*\{([^}]+)\}`).FindStringSubmatch(query)
	if len(dataMatch) >= 2 {
		pairs := regexp.MustCompile(`(\w+)\s*:\s*"([^"]*)"`).FindAllStringSubmatch(dataMatch[1], -1)
		for _, p := range pairs {
			data[p[1]] = p[2]
		}
	}

	// Extract IDs for delete
	if idsMatch := idsRe.FindStringSubmatch(query); len(idsMatch) >= 2 {
		for _, id := range strings.Split(idsMatch[1], ",") {
			id = strings.Trim(strings.TrimSpace(id), `"'`)
			if id != "" {
				data["id"] = id
				break
			}
		}
	}

	return
}

// ──────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────

// filterFields selects requested fields from a row and converts keys to camelCase.
// GraphQL queries use camelCase (terraformVersion) but DB returns snake_case (terraform_version).
func filterFields(row map[string]interface{}, fields []string) map[string]interface{} {
	if len(fields) == 0 {
		// Return all fields, converted to camelCase
		result := make(map[string]interface{}, len(row))
		for k, v := range row {
			result[snakeToCamel(k)] = v
		}
		return result
	}

	result := make(map[string]interface{})
	for _, f := range fields {
		// Try exact match first (camelCase from DB already converted)
		if v, ok := row[f]; ok {
			result[f] = v
			continue
		}
		// Try snake_case version (UI sends camelCase, DB has snake_case)
		snakeF := camelToSnake(f)
		if v, ok := row[snakeF]; ok {
			result[f] = v
		}
	}
	return result
}

// snakeToCamel converts snake_case to camelCase: "terraform_version" → "terraformVersion"
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) <= 1 {
		return s
	}
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// camelToSnake converts camelCase to snake_case: "terraformVersion" → "terraform_version"
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + 32) // toLower
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func wrapEdges(nodes []map[string]interface{}) []map[string]interface{} {
	edges := make([]map[string]interface{}, len(nodes))
	for i, node := range nodes {
		edges[i] = map[string]interface{}{"node": node}
	}
	return edges
}

func writeGQLError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // GraphQL always returns 200
	json.NewEncoder(w).Encode(graphQLResponse{
		Errors: []gqlError{{Message: msg}},
	})
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
