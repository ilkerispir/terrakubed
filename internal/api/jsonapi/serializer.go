package jsonapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────────────────────────────
// JSON:API Document types
// ──────────────────────────────────────────────────

// Document is a top-level JSON:API response.
type Document struct {
	Data     interface{}            `json:"data"`               // Resource or []Resource
	Included []Resource             `json:"included,omitempty"` // Sideloaded resources
	Meta     map[string]interface{} `json:"meta,omitempty"`
	Links    *Links                 `json:"links,omitempty"`
}

// Resource is a single JSON:API resource object.
type Resource struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id"`
	Attributes    map[string]interface{}  `json:"attributes"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
	Links         *Links                  `json:"links,omitempty"`
}

// Relationship is a JSON:API relationship object.
type Relationship struct {
	Data  interface{} `json:"data,omitempty"` // ResourceIdentifier or []ResourceIdentifier
	Links *Links      `json:"links,omitempty"`
}

// ResourceIdentifier is a minimal resource reference.
type ResourceIdentifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Links contains link URLs.
type Links struct {
	Self  string `json:"self,omitempty"`
	First string `json:"first,omitempty"`
	Last  string `json:"last,omitempty"`
	Prev  string `json:"prev,omitempty"`
	Next  string `json:"next,omitempty"`
}

// ErrorDocument is a JSON:API error response.
type ErrorDocument struct {
	Errors []Error `json:"errors"`
}

// Error is a single JSON:API error.
type Error struct {
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// ──────────────────────────────────────────────────
// Request payload (for POST/PATCH)
// ──────────────────────────────────────────────────

// RequestDocument represents a JSON:API request body.
type RequestDocument struct {
	Data RequestResource `json:"data"`
}

// RequestResource is the resource in a request.
type RequestResource struct {
	Type          string                         `json:"type"`
	ID            string                         `json:"id,omitempty"`
	Attributes    map[string]interface{}         `json:"attributes"`
	Relationships map[string]RequestRelationship `json:"relationships,omitempty"`
}

// RequestRelationship is a relationship in a request.
type RequestRelationship struct {
	Data *ResourceIdentifier `json:"data"`
}

// ──────────────────────────────────────────────────
// Serialization from DB row → JSON:API Resource
// ──────────────────────────────────────────────────

// ColumnMapping defines how a DB column maps to a JSON:API attribute.
type ColumnMapping struct {
	Column        string // DB column name
	JSONAttribute string // JSON:API attribute name (camelCase)
	IsPK          bool
	IsFK          bool
	FKRelation    string // If FK, the relationship name
	Excluded      bool   // If true, not included in attributes (e.g. join columns)
}

// ResourceConfig describes how to serialize a resource type.
type ResourceConfig struct {
	Type       string
	PKColumn   string
	Columns    []ColumnMapping
	ParentRels map[string]ParentRelConfig // relationship name → config
	ChildRels  map[string]ChildRelConfig  // relationship name → config
}

// ParentRelConfig describes a parent (to-one) relationship.
type ParentRelConfig struct {
	FKColumn   string // DB column
	TargetType string // JSON:API type of the parent
}

// ChildRelConfig describes a child (to-many) relationship.
type ChildRelConfig struct {
	TargetType string // JSON:API type of the children
	FKColumn   string // FK column in child table
}

// Serialize converts a DB row (map[string]interface{}) to a JSON:API Resource.
func Serialize(config *ResourceConfig, row map[string]interface{}, basePath string) Resource {
	res := Resource{
		Type:          config.Type,
		Attributes:    make(map[string]interface{}),
		Relationships: make(map[string]Relationship),
	}

	// Set ID
	if pk, ok := row[config.PKColumn]; ok {
		res.ID = formatID(pk)
	}

	// Set attributes
	for _, col := range config.Columns {
		if col.IsPK || col.Excluded || col.IsFK {
			continue
		}
		val, ok := row[col.Column]
		if !ok {
			continue
		}
		res.Attributes[col.JSONAttribute] = formatValue(val)
	}

	// Set parent relationships (to-one)
	for relName, relConfig := range config.ParentRels {
		fkVal, ok := row[relConfig.FKColumn]
		rel := Relationship{
			Links: &Links{
				Self: fmt.Sprintf("%s/%s/%s/relationships/%s", basePath, config.Type, res.ID, relName),
			},
		}
		if ok && fkVal != nil {
			rel.Data = ResourceIdentifier{
				Type: relConfig.TargetType,
				ID:   formatID(fkVal),
			}
		} else {
			rel.Data = nil
		}
		res.Relationships[relName] = rel
	}

	// Set child relationships (to-many) — just links, no data (loaded on demand)
	for relName := range config.ChildRels {
		res.Relationships[relName] = Relationship{
			Links: &Links{
				Self: fmt.Sprintf("%s/%s/%s/relationships/%s", basePath, config.Type, res.ID, relName),
			},
		}
	}

	// Self link
	res.Links = &Links{
		Self: fmt.Sprintf("%s/%s/%s", basePath, config.Type, res.ID),
	}

	return res
}

// SerializeList converts multiple DB rows to a JSON:API Document.
func SerializeList(config *ResourceConfig, rows []map[string]interface{}, basePath string) Document {
	resources := make([]Resource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, Serialize(config, row, basePath))
	}
	return Document{
		Data: resources,
	}
}

// SerializeSingle converts a single DB row to a JSON:API Document.
func SerializeSingle(config *ResourceConfig, row map[string]interface{}, basePath string) Document {
	res := Serialize(config, row, basePath)
	return Document{
		Data: res,
	}
}

// ──────────────────────────────────────────────────
// Deserialization from JSON:API request → DB columns
// ──────────────────────────────────────────────────

// Deserialize converts a JSON:API RequestResource to a DB row map.
func Deserialize(config *ResourceConfig, req RequestResource) map[string]interface{} {
	row := make(map[string]interface{})

	// Set ID if provided
	if req.ID != "" {
		row[config.PKColumn] = req.ID
	}

	// Map attributes back to DB columns
	attrToCol := make(map[string]string)
	for _, col := range config.Columns {
		if col.IsPK || col.IsFK || col.Excluded {
			continue
		}
		attrToCol[col.JSONAttribute] = col.Column
	}

	for attr, val := range req.Attributes {
		if colName, ok := attrToCol[attr]; ok {
			row[colName] = val
		}
	}

	// Map relationships to FK columns
	for relName, rel := range req.Relationships {
		if parentConfig, ok := config.ParentRels[relName]; ok && rel.Data != nil {
			row[parentConfig.FKColumn] = rel.Data.ID
		}
	}

	return row
}

// ──────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────

func formatID(v interface{}) string {
	switch id := v.(type) {
	case uuid.UUID:
		return id.String()
	case [16]byte:
		return uuid.UUID(id).String()
	case string:
		return id
	case int, int32, int64:
		return fmt.Sprintf("%d", id)
	default:
		return fmt.Sprintf("%v", id)
	}
}

func formatValue(v interface{}) interface{} {
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	case *time.Time:
		if val != nil {
			return val.Format(time.RFC3339)
		}
		return nil
	case uuid.UUID:
		return val.String()
	case [16]byte:
		return uuid.UUID(val).String()
	default:
		return val
	}
}

// CamelCase converts a snake_case string to camelCase.
func CamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
