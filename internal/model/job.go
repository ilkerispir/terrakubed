package model

type Command struct {
	Priority   int    `json:"priority"`
	Script     string `json:"script"`
	Runtime    string `json:"runtime,omitempty"`    // BASH or GROOVY
	BeforeInit bool   `json:"beforeInit,omitempty"` // Run before terraform init
	Before     bool   `json:"before,omitempty"`     // Run after init, before plan/apply
	After      bool   `json:"after,omitempty"`      // Run after plan/apply on success
	OnFailure  bool   `json:"onFailure,omitempty"`  // Run after plan/apply on failure
	Verbose    bool   `json:"verbose,omitempty"`     // Print script banner
}

type TerraformJob struct {
	CommandList          []Command         `json:"commandList"`
	Type                 string            `json:"type"`
	OverrideBackend      bool              `json:"overrideBackend"`
	TerraformOutput      string            `json:"terraformOutput,omitempty"`
	OrganizationId       string            `json:"organizationId"`
	WorkspaceId          string            `json:"workspaceId"`
	JobId                string            `json:"jobId"`
	StepId               string            `json:"stepId"`
	TerraformVersion     string            `json:"terraformVersion"`
	Source               string            `json:"source"`
	Branch               string            `json:"branch"`
	Folder               string            `json:"folder"`
	VcsType              string            `json:"vcsType"`
	ConnectionType       string            `json:"connectionType,omitempty"`
	AccessToken          string            `json:"accessToken"`
	ModuleSshKey         string            `json:"moduleSshKey,omitempty"`
	CommitId             string            `json:"commitId,omitempty"`
	Tofu                 bool              `json:"tofu,omitempty"`
	Refresh              bool              `json:"refresh"`
	RefreshOnly          bool              `json:"refreshOnly"`
	IgnoreError          bool              `json:"ignoreError"`
	ShowHeader           bool              `json:"showHeader"`
	EnvironmentVariables map[string]string `json:"environmentVariables"`
	Variables            map[string]string `json:"variables"`
}
