package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/ilkerispir/terrakubed/internal/model"
)

// notifySlackOnFailure sends a Slack failure notification when a terraform step fails.
// It fires automatically if SLACK_WEBHOOK_URL is set in the job's environment variables.
// No YAML changes or Java API changes are required.
func (p *JobProcessor) notifySlackOnFailure(job *model.TerraformJob) {
	webhookURL := job.EnvironmentVariables["SLACK_WEBHOOK_URL"]
	if webhookURL == "" {
		return
	}

	uiURL := job.EnvironmentVariables["TERRAKUBE_UI_URL"]
	wsURL := ""
	if uiURL != "" {
		wsURL = fmt.Sprintf("https://%s/organizations/%s/workspaces/%s/runs/%s",
			uiURL, job.OrganizationId, job.WorkspaceId, job.JobId)
	}

	var title string
	switch job.Type {
	case "terraformApply":
		title = ":fire: *Terraform Apply Failed*"
	case "terraformDestroy":
		title = ":fire: *Terraform Destroy Failed*"
	case "terraformPlanDestroy":
		title = ":x: *Terraform Plan Destroy Failed*"
	default:
		title = ":x: *Terraform Plan Failed*"
	}

	msgData := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": "#cc0000",
				"blocks": []map[string]interface{}{
					{
						"type": "section",
						"text": map[string]string{"type": "mrkdwn", "text": title},
					},
					{
						"type": "section",
						"fields": []map[string]string{
							{"type": "mrkdwn", "text": "*Workspace:*\n" + wsURL},
							{"type": "mrkdwn", "text": "*Repo:*\n" + job.Source},
						},
					},
					{"type": "divider"},
					{
						"type": "context",
						"elements": []map[string]string{
							{"type": "mrkdwn", "text": "Branch: `" + job.Branch + "`"},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(msgData)
	if err != nil {
		log.Printf("Failed to marshal Slack failure payload: %v", err)
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to send Slack failure notification: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Slack failure notification sent (status: %d)", resp.StatusCode)
}
