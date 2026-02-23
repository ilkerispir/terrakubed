package registry

import (
	"reflect"

	"github.com/ilkerispir/terrakubed/internal/api/model"
	"github.com/ilkerispir/terrakubed/internal/api/repository"
)

// RegisterAll registers all known resource types with the generic repository.
func RegisterAll(repo *repository.GenericRepository) {
	// ── Core entities ──────────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:             "organization",
		Table:            "organization",
		PKColumn:         "id",
		PKType:           "uuid",
		ModelType:        reflect.TypeOf(model.Organization{}),
		SoftDeleteColumn: "disabled",
		Parents:          map[string]repository.ParentRelation{},
		Children: map[string]repository.ChildRelation{
			"workspace":  {ChildType: "workspace", FKColumn: "organization_id"},
			"job":        {ChildType: "job", FKColumn: "organization_id"},
			"team":       {ChildType: "team", FKColumn: "organization_id"},
			"template":   {ChildType: "template", FKColumn: "organization_id"},
			"module":     {ChildType: "module", FKColumn: "organization_id"},
			"provider":   {ChildType: "provider", FKColumn: "organization_id"},
			"vcs":        {ChildType: "vcs", FKColumn: "organization_id"},
			"ssh":        {ChildType: "ssh", FKColumn: "organization_id"},
			"agent":      {ChildType: "agent", FKColumn: "organization_id"},
			"globalvar":  {ChildType: "globalvar", FKColumn: "organization_id"},
			"tag":        {ChildType: "tag", FKColumn: "organization_id"},
			"collection": {ChildType: "collection", FKColumn: "organization_id"},
			"project":    {ChildType: "project", FKColumn: "organization_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:             "workspace",
		Table:            "workspace",
		PKColumn:         "id",
		PKType:           "uuid",
		ModelType:        reflect.TypeOf(model.Workspace{}),
		SoftDeleteColumn: "deleted",
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
			"vcs":          {FKColumn: "vcs_id", ParentType: "vcs"},
			"ssh":          {FKColumn: "ssh_id", ParentType: "ssh"},
			"project":      {FKColumn: "project_id", ParentType: "project"},
			"agent":        {FKColumn: "agent_id", ParentType: "agent"},
		},
		Children: map[string]repository.ChildRelation{
			"job":          {ChildType: "job", FKColumn: "workspace_id"},
			"variable":     {ChildType: "variable", FKColumn: "workspace_id"},
			"history":      {ChildType: "history", FKColumn: "workspace_id"},
			"schedule":     {ChildType: "schedule", FKColumn: "workspace_id"},
			"workspaceTag": {ChildType: "workspacetag", FKColumn: "workspace_id"},
			"access":       {ChildType: "access", FKColumn: "workspace_id"},
			"reference":    {ChildType: "reference", FKColumn: "workspace_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "job",
		Table:     "job",
		PKColumn:  "id",
		PKType:    "int",
		ModelType: reflect.TypeOf(model.Job{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
			"workspace":    {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{
			"step":    {ChildType: "step", FKColumn: "job_id"},
			"address": {ChildType: "address", FKColumn: "job_id"},
		},
		DefaultValues: map[string]interface{}{
			"status":       "pending",
			"tcl":          "{}",
			"refresh":      true,
			"refresh_only": false,
			"plan_changes": true,
			"auto_apply":   false,
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "step",
		Table:     "step",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Step{}),
		Parents: map[string]repository.ParentRelation{
			"job": {FKColumn: "job_id", ParentType: "job"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Teams ──────────────────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "team",
		Table:     "team",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Team{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Templates ──────────────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "template",
		Table:     "template",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Template{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── VCS & SSH ──────────────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "vcs",
		Table:     "vcs",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Vcs{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{
			"workspace": {ChildType: "workspace", FKColumn: "vcs_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "ssh",
		Table:     "ssh",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Ssh{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "github_app_token",
		Table:     "github_app_token",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.GitHubAppToken{}),
		Parents:   map[string]repository.ParentRelation{},
		Children:  map[string]repository.ChildRelation{},
	})

	// ── Modules & Providers ────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "module",
		Table:     "module",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Module{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
			"vcs":          {FKColumn: "vcs_id", ParentType: "vcs"},
			"ssh":          {FKColumn: "ssh_id", ParentType: "ssh"},
		},
		Children: map[string]repository.ChildRelation{
			"version": {ChildType: "module_version", FKColumn: "module_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "module_version",
		Table:     "module_version",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.ModuleVersion{}),
		Parents: map[string]repository.ParentRelation{
			"module": {FKColumn: "module_id", ParentType: "module"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "provider",
		Table:     "provider",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.ProviderEntity{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{
			"version": {ChildType: "version", FKColumn: "provider_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "version",
		Table:     "version",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.ProviderVersion{}),
		Parents: map[string]repository.ParentRelation{
			"provider": {FKColumn: "provider_id", ParentType: "provider"},
		},
		Children: map[string]repository.ChildRelation{
			"implementation": {ChildType: "implementation", FKColumn: "version_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "implementation",
		Table:     "implementation",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Implementation{}),
		Parents: map[string]repository.ParentRelation{
			"version": {FKColumn: "version_id", ParentType: "version"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Variables ──────────────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "variable",
		Table:     "variable",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Variable{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "globalvar",
		Table:     "globalvar",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Globalvar{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Workspace sub-entities ─────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "history",
		Table:     "history",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.History{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "schedule",
		Table:     "schedule",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Schedule{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "access",
		Table:     "access",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Access{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "content",
		Table:     "content",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Content{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "workspacetag",
		Table:     "workspacetag",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.WorkspaceTag{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Collections, Tags, Projects ────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "tag",
		Table:     "tag",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Tag{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "collection",
		Table:     "collection",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Collection{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{
			"reference": {ChildType: "reference", FKColumn: "collection_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "reference",
		Table:     "reference",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Reference{}),
		Parents: map[string]repository.ParentRelation{
			"collection": {FKColumn: "collection_id", ParentType: "collection"},
			"workspace":  {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "project",
		Table:     "project",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Project{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Agents & Webhooks ──────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "agent",
		Table:     "agent",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Agent{}),
		Parents: map[string]repository.ParentRelation{
			"organization": {FKColumn: "organization_id", ParentType: "organization"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "webhook",
		Table:     "webhook",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Webhook{}),
		Parents: map[string]repository.ParentRelation{
			"workspace": {FKColumn: "workspace_id", ParentType: "workspace"},
		},
		Children: map[string]repository.ChildRelation{
			"events": {ChildType: "webhook_event", FKColumn: "webhook_id"},
		},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "webhook_event",
		Table:     "webhook_event",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.WebhookEvent{}),
		Parents: map[string]repository.ParentRelation{
			"webhook": {FKColumn: "webhook_id", ParentType: "webhook"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Job sub-entities ───────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "address",
		Table:     "address",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Address{}),
		Parents: map[string]repository.ParentRelation{
			"job": {FKColumn: "job_id", ParentType: "job"},
		},
		Children: map[string]repository.ChildRelation{},
	})

	// ── Auth ───────────────────────────────────────

	repo.Register(&repository.ResourceMeta{
		Type:      "pat",
		Table:     "pat",
		PKColumn:  "id",
		PKType:    "uuid",
		ModelType: reflect.TypeOf(model.Pat{}),
		Parents:   map[string]repository.ParentRelation{},
		Children:  map[string]repository.ChildRelation{},
	})

	repo.Register(&repository.ResourceMeta{
		Type:      "action",
		Table:     "action",
		PKColumn:  "id",
		PKType:    "string",
		ModelType: reflect.TypeOf(model.Action{}),
		Parents:   map[string]repository.ParentRelation{},
		Children:  map[string]repository.ChildRelation{},
	})
}
