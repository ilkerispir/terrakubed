package model

type Command struct {
	Priority int    `json:"priority"`
	Script   string `json:"script"`
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
	AccessToken          string            `json:"accessToken"`
	EnvironmentVariables map[string]string `json:"environmentVariables"`
	Variables            map[string]string `json:"variables"`
}
