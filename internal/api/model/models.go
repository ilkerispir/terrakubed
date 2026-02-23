package model

import (
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────────────────────────────
// Enums
// ──────────────────────────────────────────────────

type JobStatus string

const (
	JobStatusPending         JobStatus = "pending"
	JobStatusWaitingApproval JobStatus = "waitingApproval"
	JobStatusApproved        JobStatus = "approved"
	JobStatusQueue           JobStatus = "queue"
	JobStatusRunning         JobStatus = "running"
	JobStatusCompleted       JobStatus = "completed"
	JobStatusNoChanges       JobStatus = "noChanges"
	JobStatusNotExecuted     JobStatus = "notExecuted"
	JobStatusRejected        JobStatus = "rejected"
	JobStatusCancelled       JobStatus = "cancelled"
	JobStatusFailed          JobStatus = "failed"
	JobStatusUnknown         JobStatus = "unknown"
	JobStatusNeverExecuted   JobStatus = "NeverExecuted"
)

type LogStatus string

const (
	LogStatusBegin     LogStatus = "BEGIN"
	LogStatusRunning   LogStatus = "RUNNING"
	LogStatusCompleted LogStatus = "COMPLETED"
	LogStatusUnknown   LogStatus = "UNKNOWN"
)

type ExecutionMode string

const (
	ExecutionModeLocal  ExecutionMode = "local"
	ExecutionModeRemote ExecutionMode = "remote"
)

type VcsType string

const (
	VcsTypeGitHub      VcsType = "GITHUB"
	VcsTypeGitLab      VcsType = "GITLAB"
	VcsTypeBitbucket   VcsType = "BITBUCKET"
	VcsTypeAzureDevOps VcsType = "AZURE_DEVOPS"
	VcsTypeAzureSPMI   VcsType = "AZURE_SP_MI"
	VcsTypePublic      VcsType = "PUBLIC"
)

type VcsConnectionType string

const (
	VcsConnectionTypeOAuth VcsConnectionType = "OAUTH"
)

type VcsStatus string

const (
	VcsStatusPending VcsStatus = "PENDING"
)

type SshType string

const (
	SshTypeRSA     SshType = "rsa"
	SshTypeED25519 SshType = "ed25519"
)

type VariableCategory string

const (
	VariableCategoryTerraform VariableCategory = "TERRAFORM"
	VariableCategoryEnv       VariableCategory = "ENV"
)

type AddressType string

const (
	AddressTypeResource AddressType = "resource"
	AddressTypeData     AddressType = "data"
)

type ArchiveType string

const (
	ArchiveTypeState ArchiveType = "state"
	ArchiveTypePlan  ArchiveType = "plan"
)

type WebhookEventType string

const (
	WebhookEventTypePush WebhookEventType = "PUSH"
	WebhookEventTypeTag  WebhookEventType = "TAG"
)

// ──────────────────────────────────────────────────
// Audit fields (embedded in many entities)
// ──────────────────────────────────────────────────

type AuditFields struct {
	CreatedDate *time.Time `json:"createdDate,omitempty" db:"created_date"`
	CreatedBy   string     `json:"createdBy,omitempty"   db:"created_by"`
	UpdatedDate *time.Time `json:"updatedDate,omitempty" db:"updated_date"`
	UpdatedBy   string     `json:"updatedBy,omitempty"   db:"updated_by"`
}

// ──────────────────────────────────────────────────
// Core entities
// ──────────────────────────────────────────────────

// Organization — table "organization"
type Organization struct {
	ID            uuid.UUID     `json:"id"            db:"id"`
	Name          string        `json:"name"          db:"name"`
	Description   string        `json:"description"   db:"description"`
	Disabled      bool          `json:"disabled"      db:"disabled"`
	ExecutionMode ExecutionMode `json:"executionMode" db:"execution_mode"`
	Icon          string        `json:"icon"          db:"icon"`
}

// Workspace — table "workspace"
type Workspace struct {
	AuditFields
	ID               uuid.UUID     `json:"id"               db:"id"`
	Name             string        `json:"name"             db:"name"`
	Description      string        `json:"description"      db:"description"`
	Source           string        `json:"source"           db:"source"`
	Branch           string        `json:"branch"           db:"branch"`
	Folder           string        `json:"folder"           db:"folder"`
	Locked           bool          `json:"locked"           db:"locked"`
	Deleted          bool          `json:"deleted"          db:"deleted"`
	AllowRemoteApply bool          `json:"allowRemoteApply" db:"allow_remote_apply"`
	DefaultTemplate  string        `json:"defaultTemplate"  db:"default_template"`
	LockDescription  string        `json:"lockDescription"  db:"lock_description"`
	IacType          string        `json:"iacType"          db:"iac_type"`
	ModuleSshKey     string        `json:"moduleSshKey"     db:"module_ssh_key"`
	TerraformVersion string        `json:"terraformVersion" db:"terraform_version"`
	ExecutionMode    ExecutionMode `json:"executionMode"    db:"execution_mode"`
	OrganizationID   uuid.UUID     `json:"organizationId"   db:"organization_id"`
	ProjectID        *uuid.UUID    `json:"projectId"        db:"project_id"`
	VcsID            *uuid.UUID    `json:"vcsId"            db:"vcs_id"`
	SshID            *uuid.UUID    `json:"sshId"            db:"ssh_id"`
	AgentID          *uuid.UUID    `json:"agentId"          db:"agent_id"`
}

// Job — table "job" (integer PK, auto-increment)
type Job struct {
	AuditFields
	ID                int       `json:"id"                db:"id"`
	Comments          string    `json:"comments"          db:"comments"`
	Status            JobStatus `json:"status"            db:"status"`
	Output            string    `json:"output"            db:"output"`
	CommitID          string    `json:"commitId"          db:"commit_id"`
	AutoApply         bool      `json:"-"                 db:"auto_apply"`
	TerraformPlan     string    `json:"terraformPlan"     db:"terraform_plan"`
	ApprovalTeam      string    `json:"approvalTeam"      db:"approval_team"`
	Tcl               string    `json:"tcl"               db:"tcl"`
	OverrideSource    string    `json:"-"                 db:"override_source"`
	OverrideBranch    string    `json:"overrideBranch"    db:"override_branch"`
	TemplateReference string    `json:"templateReference" db:"template_reference"`
	Via               string    `json:"via"               db:"via"`
	Refresh           bool      `json:"refresh"           db:"refresh"`
	PlanChanges       bool      `json:"planChanges"       db:"plan_changes"`
	RefreshOnly       bool      `json:"refreshOnly"       db:"refresh_only"`
	OrganizationID    uuid.UUID `json:"organizationId"    db:"organization_id"`
	WorkspaceID       uuid.UUID `json:"workspaceId"       db:"workspace_id"`
}

// Step — table "step"
type Step struct {
	ID         uuid.UUID `json:"id"         db:"id"`
	StepNumber int       `json:"stepNumber" db:"step_number"`
	Name       string    `json:"name"       db:"name"`
	Status     JobStatus `json:"status"     db:"status"`
	LogStatus  LogStatus `json:"-"          db:"log_status"`
	Output     string    `json:"output"     db:"output"`
	JobID      int       `json:"jobId"      db:"job_id"`
}

// Team — table "team"
type Team struct {
	ID               uuid.UUID `json:"id"              db:"id"`
	Name             string    `json:"name"            db:"name"`
	ManageState      bool      `json:"manageState"     db:"manage_state"`
	ManageCollection bool      `json:"manageCollection" db:"manage_collection"`
	ManageJob        bool      `json:"manageJob"       db:"manage_job"`
	ManageWorkspace  bool      `json:"manageWorkspace" db:"manage_workspace"`
	ManageModule     bool      `json:"manageModule"    db:"manage_module"`
	ManageProvider   bool      `json:"manageProvider"  db:"manage_provider"`
	ManageVcs        bool      `json:"manageVcs"       db:"manage_vcs"`
	ManageTemplate   bool      `json:"manageTemplate"  db:"manage_template"`
	OrganizationID   uuid.UUID `json:"organizationId"  db:"organization_id"`
}

// Template — table "template"
type Template struct {
	AuditFields
	ID             uuid.UUID `json:"id"             db:"id"`
	Name           string    `json:"name"           db:"name"`
	Description    string    `json:"description"    db:"description"`
	Version        string    `json:"version"        db:"version"`
	Tcl            string    `json:"tcl"            db:"tcl"`
	OrganizationID uuid.UUID `json:"organizationId" db:"organization_id"`
}

// ──────────────────────────────────────────────────
// VCS & SSH
// ──────────────────────────────────────────────────

// Vcs — table "vcs"
type Vcs struct {
	AuditFields
	ID              uuid.UUID         `json:"id"              db:"id"`
	Name            string            `json:"name"            db:"name"`
	VcsType         VcsType           `json:"vcsType"         db:"vcs_type"`
	Description     string            `json:"description"     db:"description"`
	ClientID        string            `json:"clientId"        db:"client_id"`
	Callback        string            `json:"callback"        db:"callback"`
	Endpoint        string            `json:"endpoint"        db:"endpoint"`
	ApiURL          string            `json:"apiUrl"          db:"api_url"`
	ClientSecret    string            `json:"clientSecret"    db:"client_secret"`
	PrivateKey      string            `json:"privateKey"      db:"private_key"`
	ConnectionType  VcsConnectionType `json:"connectionType"  db:"connection_type"`
	Status          VcsStatus         `json:"status"          db:"status"`
	AccessToken     string            `json:"accessToken"     db:"access_token"`
	RefreshToken    string            `json:"-"               db:"refresh_token"`
	TokenExpiration *time.Time        `json:"-"               db:"token_expiration"`
	RedirectURL     string            `json:"redirectUrl"     db:"redirect_url"`
	OrganizationID  uuid.UUID         `json:"organizationId"  db:"organization_id"`
}

// GitHubAppToken — table "github_app_token"
type GitHubAppToken struct {
	AuditFields
	ID             uuid.UUID `json:"id"             db:"id"`
	Owner          string    `json:"owner"          db:"owner"`
	InstallationID string    `json:"installationId" db:"installation_id"`
	Token          string    `json:"token"          db:"token"`
	AppID          string    `json:"appId"          db:"app_id"`
}

// Ssh — table "ssh"
type Ssh struct {
	AuditFields
	ID             uuid.UUID `json:"id"             db:"id"`
	Name           string    `json:"name"           db:"name"`
	Description    string    `json:"description"    db:"description"`
	PrivateKey     string    `json:"privateKey"     db:"private_key"`
	SshType        SshType   `json:"sshType"        db:"ssh_type"`
	OrganizationID uuid.UUID `json:"organizationId" db:"organization_id"`
}

// ──────────────────────────────────────────────────
// Module & Provider registry
// ──────────────────────────────────────────────────

// Module — table "module"
type Module struct {
	AuditFields
	ID               uuid.UUID  `json:"id"               db:"id"`
	Name             string     `json:"name"             db:"name"`
	Description      string     `json:"description"      db:"description"`
	Provider         string     `json:"provider"         db:"provider"`
	Source           string     `json:"source"           db:"source"`
	TagPrefix        string     `json:"tagPrefix"        db:"tag_prefix"`
	Folder           string     `json:"folder"           db:"folder"`
	DownloadQuantity int        `json:"downloadQuantity" db:"download_quantity"`
	LatestVersion    string     `json:"latestVersion"    db:"latest_version"`
	OrganizationID   uuid.UUID  `json:"organizationId"   db:"organization_id"`
	VcsID            *uuid.UUID `json:"vcsId"            db:"vcs_id"`
	SshID            *uuid.UUID `json:"sshId"            db:"ssh_id"`
}

// ModuleVersion — table "module_version"
type ModuleVersion struct {
	ID       uuid.UUID `json:"id"       db:"id"`
	Version  string    `json:"version"  db:"version"`
	Commit   string    `json:"commit"   db:"commit_info"`
	ModuleID uuid.UUID `json:"moduleId" db:"module_id"`
}

// Provider — table "provider"
type ProviderEntity struct {
	ID                uuid.UUID `json:"id"                db:"id"`
	Name              string    `json:"name"              db:"name"`
	Description       string    `json:"description"       db:"description"`
	Imported          bool      `json:"imported"          db:"imported"`
	RegistryNamespace string    `json:"registryNamespace" db:"registry_namespace"`
	OrganizationID    uuid.UUID `json:"organizationId"    db:"organization_id"`
}

// ProviderVersion — table "version"
type ProviderVersion struct {
	ID            uuid.UUID `json:"id"            db:"id"`
	VersionNumber string    `json:"versionNumber" db:"version_number"`
	Protocols     string    `json:"protocols"     db:"protocols"`
	ProviderID    uuid.UUID `json:"providerId"    db:"provider_id"`
}

// Implementation — table "implementation"
type Implementation struct {
	ID                  uuid.UUID `json:"id"                   db:"id"`
	OS                  string    `json:"os"                   db:"os"`
	Arch                string    `json:"arch"                 db:"arch"`
	Filename            string    `json:"filename"             db:"filename"`
	DownloadURL         string    `json:"downloadUrl"          db:"download_url"`
	ShasumsURL          string    `json:"shasumsUrl"           db:"shasums_url"`
	ShasumsSignatureURL string    `json:"shasumsSignatureUrl"  db:"shasums_signature_url"`
	Shasum              string    `json:"shasum"               db:"shasum"`
	KeyID               string    `json:"keyId"                db:"key_id"`
	AsciiArmor          string    `json:"asciiArmor"           db:"ascii_armor"`
	TrustSignature      string    `json:"trustSignature"       db:"trust_signature"`
	SourceField         string    `json:"source"               db:"source"`
	SourceURL           string    `json:"sourceUrl"            db:"source_url"`
	VersionID           uuid.UUID `json:"versionId"            db:"version_id"`
}

// ──────────────────────────────────────────────────
// Variables & Globals
// ──────────────────────────────────────────────────

// Variable — table "variable" (workspace-scoped)
type Variable struct {
	ID          uuid.UUID        `json:"id"          db:"id"`
	Key         string           `json:"key"         db:"variable_key"`
	Value       string           `json:"value"       db:"variable_value"`
	Description string           `json:"description" db:"variable_description"`
	Category    VariableCategory `json:"category"    db:"variable_category"`
	Sensitive   bool             `json:"sensitive"   db:"sensitive"`
	HCL         bool             `json:"hcl"         db:"hcl"`
	WorkspaceID uuid.UUID        `json:"workspaceId" db:"workspace_id"`
}

// Globalvar — table "globalvar" (organization-scoped)
type Globalvar struct {
	ID             uuid.UUID        `json:"id"             db:"id"`
	Key            string           `json:"key"            db:"variable_key"`
	Value          string           `json:"value"          db:"variable_value"`
	Description    string           `json:"description"    db:"variable_description"`
	Category       VariableCategory `json:"category"       db:"variable_category"`
	Sensitive      bool             `json:"sensitive"      db:"sensitive"`
	HCL            bool             `json:"hcl"            db:"hcl"`
	OrganizationID uuid.UUID        `json:"organizationId" db:"organization_id"`
}

// ──────────────────────────────────────────────────
// Workspace sub-entities
// ──────────────────────────────────────────────────

// History — table "history" (terraform state history)
type History struct {
	AuditFields
	ID           uuid.UUID `json:"id"           db:"id"`
	JobReference string    `json:"jobReference" db:"job_reference"`
	Output       string    `json:"output"       db:"output"`
	Serial       int       `json:"serial"       db:"serial"`
	MD5          string    `json:"md5"          db:"md5"`
	Lineage      string    `json:"lineage"      db:"lineage"`
	WorkspaceID  uuid.UUID `json:"workspaceId"  db:"workspace_id"`
}

// Archive — table "temp_archive"
type Archive struct {
	ID        uuid.UUID   `json:"id"        db:"id"`
	Type      ArchiveType `json:"type"      db:"type"`
	HistoryID uuid.UUID   `json:"historyId" db:"history_id"`
}

// Schedule — table "schedule"
type Schedule struct {
	AuditFields
	ID                uuid.UUID `json:"id"                db:"id"`
	Cron              string    `json:"cron"              db:"cron"`
	Tcl               string    `json:"tcl"               db:"tcl"`
	TemplateReference string    `json:"templateReference" db:"template_reference"`
	Description       string    `json:"description"       db:"description"`
	Enabled           bool      `json:"enabled"           db:"enabled"`
	WorkspaceID       uuid.UUID `json:"workspaceId"       db:"workspace_id"`
}

// Access — table "access" (workspace-level team access)
type Access struct {
	ID              uuid.UUID `json:"id"              db:"id"`
	Name            string    `json:"name"            db:"name"`
	ManageState     bool      `json:"manageState"     db:"manage_state"`
	ManageJob       bool      `json:"manageJob"       db:"manage_job"`
	ManageWorkspace bool      `json:"manageWorkspace" db:"manage_workspace"`
	WorkspaceID     uuid.UUID `json:"workspaceId"     db:"workspace_id"`
}

// Content — table "content"
type Content struct {
	ID            uuid.UUID `json:"id"            db:"id"`
	AutoQueueRuns bool      `json:"autoQueueRuns" db:"auto_queue_runs"`
	Speculative   bool      `json:"speculative"   db:"speculative"`
	Status        string    `json:"status"        db:"status"`
	Source        string    `json:"source"        db:"source"`
	WorkspaceID   uuid.UUID `json:"workspaceId"   db:"workspace_id"`
}

// WorkspaceTag — table "workspacetag"
type WorkspaceTag struct {
	AuditFields
	ID          uuid.UUID `json:"id"          db:"id"`
	TagID       string    `json:"tagId"       db:"tag_id"`
	WorkspaceID uuid.UUID `json:"workspaceId" db:"workspace_id"`
}

// ──────────────────────────────────────────────────
// Job sub-entities
// ──────────────────────────────────────────────────

// Address — table "address"
type Address struct {
	AuditFields
	ID    uuid.UUID   `json:"id"   db:"id"`
	Name  string      `json:"name" db:"name"`
	Type  AddressType `json:"-"    db:"type"`
	JobID int         `json:"jobId" db:"job_id"`
}

// ──────────────────────────────────────────────────
// Collections & Tags
// ──────────────────────────────────────────────────

// Tag — table "tag"
type Tag struct {
	AuditFields
	ID             uuid.UUID `json:"id"             db:"id"`
	Name           string    `json:"name"           db:"name"`
	OrganizationID uuid.UUID `json:"organizationId" db:"organization_id"`
}

// Collection — table "collection"
type Collection struct {
	AuditFields
	ID             uuid.UUID `json:"id"             db:"id"`
	Name           string    `json:"name"           db:"name"`
	Description    string    `json:"description"    db:"description"`
	Priority       int       `json:"priority"       db:"priority"`
	OrganizationID uuid.UUID `json:"organizationId" db:"organization_id"`
}

// Reference — table "reference"
type Reference struct {
	AuditFields
	ID           uuid.UUID `json:"id"           db:"id"`
	Description  string    `json:"description"  db:"description"`
	CollectionID uuid.UUID `json:"collectionId" db:"collection_id"`
	WorkspaceID  uuid.UUID `json:"workspaceId"  db:"workspace_id"`
}

// ──────────────────────────────────────────────────
// Project, Agent, Webhook
// ──────────────────────────────────────────────────

// Project — table "project"
type Project struct {
	AuditFields
	ID             uuid.UUID `json:"id"             db:"id"`
	Name           string    `json:"name"           db:"name"`
	Description    string    `json:"description"    db:"description"`
	OrganizationID uuid.UUID `json:"organizationId" db:"organization_id"`
}

// Agent — table "agent"
type Agent struct {
	ID             uuid.UUID `json:"id"             db:"id"`
	Name           string    `json:"name"           db:"name"`
	URL            string    `json:"url"            db:"url"`
	Description    string    `json:"description"    db:"description"`
	OrganizationID uuid.UUID `json:"organizationId" db:"organization_id"`
}

// Webhook — table "webhook"
type Webhook struct {
	AuditFields
	ID           uuid.UUID `json:"id"           db:"id"`
	RemoteHookID string    `json:"remoteHookId" db:"remote_hook_id"`
	WorkspaceID  uuid.UUID `json:"workspaceId"  db:"workspace_id"`
}

// WebhookEvent — table "webhook_event"
type WebhookEvent struct {
	AuditFields
	ID         uuid.UUID        `json:"id"         db:"id"`
	Branch     string           `json:"branch"     db:"branch"`
	Path       string           `json:"path"       db:"path"`
	TemplateID string           `json:"templateId" db:"template_id"`
	Event      WebhookEventType `json:"event"      db:"event"`
	Priority   int              `json:"priority"   db:"priority"`
	WebhookID  uuid.UUID        `json:"webhookId"  db:"webhook_id"`
}

// ──────────────────────────────────────────────────
// Auth & Tokens
// ──────────────────────────────────────────────────

// Pat — table "pat" (Personal Access Token)
type Pat struct {
	AuditFields
	ID          uuid.UUID `json:"id"          db:"id"`
	Days        int       `json:"days"        db:"days"`
	Deleted     bool      `json:"deleted"     db:"deleted"`
	Description string    `json:"description" db:"description"`
}

// Action — table "action" (string PK)
type Action struct {
	AuditFields
	ID              string `json:"id"              db:"id"`
	Type            string `json:"type"            db:"type"`
	ActionField     string `json:"action"          db:"action"`
	DisplayCriteria string `json:"displayCriteria" db:"display_criteria"`
	Name            string `json:"name"            db:"name"`
	Label           string `json:"label"           db:"label"`
	Category        string `json:"category"        db:"category"`
	Description     string `json:"description"     db:"description"`
	Version         string `json:"version"         db:"version"`
	Active          bool   `json:"active"          db:"active"`
}
