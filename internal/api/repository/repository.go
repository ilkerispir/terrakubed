package repository

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResourceMeta describes a JSON:API resource type and its database mapping.
type ResourceMeta struct {
	// JSON:API type name, e.g. "organization"
	Type string
	// Database table name
	Table string
	// Name of the primary key column
	PKColumn string
	// PKType: "uuid", "int", or "string"
	PKType string
	// Go struct type (used via reflection)
	ModelType reflect.Type
	// Columns in the database (derived from db tags)
	Columns []string
	// Map from db column name → struct field index
	FieldMap map[string]int
	// Parent relationships: JSON:API relation name → FK column + parent type
	Parents map[string]ParentRelation
	// Children relationships: JSON:API relation name → child info
	Children map[string]ChildRelation
	// Soft-delete column (if any); rows where this column = true are excluded
	SoftDeleteColumn string
}

// ParentRelation describes a ManyToOne/OneToOne FK relationship.
type ParentRelation struct {
	FKColumn   string // e.g. "organization_id"
	ParentType string // e.g. "organization"
}

// ChildRelation describes a OneToMany relationship (resolved via sub-queries).
type ChildRelation struct {
	ChildType string // e.g. "workspace"
	FKColumn  string // FK column in child table, e.g. "organization_id"
}

// GenericRepository provides CRUD operations for any registered resource type.
type GenericRepository struct {
	pool      *pgxpool.Pool
	resources map[string]*ResourceMeta
}

// NewGenericRepository creates a new GenericRepository.
func NewGenericRepository(pool *pgxpool.Pool) *GenericRepository {
	return &GenericRepository{
		pool:      pool,
		resources: make(map[string]*ResourceMeta),
	}
}

// Register registers a ResourceMeta for a given JSON:API type.
func (r *GenericRepository) Register(meta *ResourceMeta) {
	// Build column list and field map from struct db tags
	meta.Columns = nil
	meta.FieldMap = make(map[string]int)
	for i := 0; i < meta.ModelType.NumField(); i++ {
		field := meta.ModelType.Field(i)
		// Handle embedded structs (e.g. AuditFields)
		if field.Anonymous {
			for j := 0; j < field.Type.NumField(); j++ {
				subField := field.Type.Field(j)
				dbTag := subField.Tag.Get("db")
				if dbTag != "" && dbTag != "-" {
					meta.Columns = append(meta.Columns, dbTag)
					// Use a composite index: base field count + sub index
					// We'll handle this via a flat scan approach
				}
			}
			continue
		}
		dbTag := field.Tag.Get("db")
		if dbTag != "" && dbTag != "-" {
			meta.Columns = append(meta.Columns, dbTag)
			meta.FieldMap[dbTag] = i
		}
	}

	r.resources[meta.Type] = meta
	log.Printf("Registered resource type: %s → table: %s (%d columns)", meta.Type, meta.Table, len(meta.Columns))
}

// GetMeta returns the ResourceMeta for a given type.
func (r *GenericRepository) GetMeta(resourceType string) (*ResourceMeta, bool) {
	meta, ok := r.resources[resourceType]
	return meta, ok
}

// AllMetas returns all registered ResourceMetas.
func (r *GenericRepository) AllMetas() map[string]*ResourceMeta {
	return r.resources
}

// ──────────────────────────────────────────────────
// Query helpers
// ──────────────────────────────────────────────────

// ListParams holds parameters for listing resources.
type ListParams struct {
	// Filter by parent FK, e.g. WHERE organization_id = $1
	ParentFK string
	ParentID interface{}
	// Additional WHERE filters: column → value
	Filters map[string]interface{}
	// Sorting: column name, prefix with "-" for DESC
	Sort string
	// Pagination
	PageSize   int
	PageOffset int
}

// List returns all rows for a resource type, applying filters and pagination.
func (r *GenericRepository) List(ctx context.Context, resourceType string, params ListParams) ([]map[string]interface{}, error) {
	meta, ok := r.resources[resourceType]
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}

	// Build SELECT
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(meta.Columns, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(meta.Table)

	var args []interface{}
	argIdx := 1
	var conditions []string

	// Soft-delete filter
	if meta.SoftDeleteColumn != "" {
		conditions = append(conditions, fmt.Sprintf("%s = false", meta.SoftDeleteColumn))
	}

	// Parent FK filter
	if params.ParentFK != "" && params.ParentID != nil {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", params.ParentFK, argIdx))
		args = append(args, params.ParentID)
		argIdx++
	}

	// Additional filters
	for col, val := range params.Filters {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	if len(conditions) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	// Sorting
	if params.Sort != "" {
		if strings.HasPrefix(params.Sort, "-") {
			sb.WriteString(fmt.Sprintf(" ORDER BY %s DESC", strings.TrimPrefix(params.Sort, "-")))
		} else {
			sb.WriteString(fmt.Sprintf(" ORDER BY %s ASC", params.Sort))
		}
	}

	// Pagination
	if params.PageSize > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", params.PageSize))
		if params.PageOffset > 0 {
			sb.WriteString(fmt.Sprintf(" OFFSET %d", params.PageOffset))
		}
	}

	rows, err := r.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	return scanRows(rows, meta.Columns)
}

// FindByID returns a single row by primary key.
func (r *GenericRepository) FindByID(ctx context.Context, resourceType string, id interface{}) (map[string]interface{}, error) {
	meta, ok := r.resources[resourceType]
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(meta.Columns, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(meta.Table)
	sb.WriteString(fmt.Sprintf(" WHERE %s = $1", meta.PKColumn))

	if meta.SoftDeleteColumn != "" {
		sb.WriteString(fmt.Sprintf(" AND %s = false", meta.SoftDeleteColumn))
	}

	rows, err := r.pool.Query(ctx, sb.String(), id)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	results, err := scanRows(rows, meta.Columns)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil // Not found
	}
	return results[0], nil
}

// Create inserts a new row and returns the generated ID.
func (r *GenericRepository) Create(ctx context.Context, resourceType string, data map[string]interface{}) (interface{}, error) {
	meta, ok := r.resources[resourceType]
	if !ok {
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}

	var cols []string
	var placeholders []string
	var args []interface{}
	argIdx := 1

	for col, val := range data {
		// Skip PK for auto-generated IDs
		if col == meta.PKColumn && val == nil {
			continue
		}
		cols = append(cols, col)
		placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
		args = append(args, val)
		argIdx++
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
		meta.Table,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		meta.PKColumn,
	)

	var id interface{}
	err := r.pool.QueryRow(ctx, query, args...).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert failed: %w", err)
	}
	return id, nil
}

// Update patches an existing row by primary key.
func (r *GenericRepository) Update(ctx context.Context, resourceType string, id interface{}, data map[string]interface{}) error {
	meta, ok := r.resources[resourceType]
	if !ok {
		return fmt.Errorf("unknown resource type: %s", resourceType)
	}

	var setClauses []string
	var args []interface{}
	argIdx := 1

	for col, val := range data {
		if col == meta.PKColumn {
			continue // Don't update PK
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	if len(setClauses) == 0 {
		return nil // Nothing to update
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d",
		meta.Table,
		strings.Join(setClauses, ", "),
		meta.PKColumn,
		argIdx,
	)

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	return nil
}

// Delete removes a row by primary key (hard delete).
func (r *GenericRepository) Delete(ctx context.Context, resourceType string, id interface{}) error {
	meta, ok := r.resources[resourceType]
	if !ok {
		return fmt.Errorf("unknown resource type: %s", resourceType)
	}

	// If soft-delete, set the flag instead
	if meta.SoftDeleteColumn != "" {
		query := fmt.Sprintf("UPDATE %s SET %s = true WHERE %s = $1",
			meta.Table, meta.SoftDeleteColumn, meta.PKColumn)
		_, err := r.pool.Exec(ctx, query, id)
		return err
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE %s = $1", meta.Table, meta.PKColumn)
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// ──────────────────────────────────────────────────
// Row scanning
// ──────────────────────────────────────────────────

func scanRows(rows pgx.Rows, columns []string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}
