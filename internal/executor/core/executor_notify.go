package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/ilkerispir/terrakubed/internal/model"
)

// slackEnabled returns (webhookURL, true) if Slack notifications are active for this job.
// Requires both ENABLE_SLACK_NOTIFICATIONS=true and SLACK_WEBHOOK_URL to be set.
func (p *JobProcessor) slackEnabled(job *model.TerraformJob) (string, bool) {
	if job.EnvironmentVariables["ENABLE_SLACK_NOTIFICATIONS"] != "true" {
		return "", false
	}
	url := job.EnvironmentVariables["SLACK_WEBHOOK_URL"]
	if url == "" {
		return "", false
	}
	return url, true
}

// slackSend builds and POSTs a minimal Slack attachment message.
func (p *JobProcessor) slackSend(webhookURL, color, title string, job *model.TerraformJob) {
	uiURL := job.EnvironmentVariables["TERRAKUBE_UI_URL"]
	wsName := job.EnvironmentVariables["workspaceName"]
	if wsName == "" {
		wsName = job.WorkspaceId
	}

	wsText := wsName
	if uiURL != "" {
		runURL := fmt.Sprintf("https://%s/organizations/%s/workspaces/%s/runs/%s",
			uiURL, job.OrganizationId, job.WorkspaceId, job.JobId)
		wsText = fmt.Sprintf("<%s|%s>", runURL, wsName)
	}

	msg := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"blocks": []map[string]interface{}{
					{
						"type": "section",
						"text": map[string]string{"type": "mrkdwn", "text": title},
					},
					{
						"type": "section",
						"fields": []map[string]string{
							{"type": "mrkdwn", "text": "*Workspace:*\n" + wsText},
							{"type": "mrkdwn", "text": "*Repo:*\n" + job.Source},
						},
					},
					{"type": "divider"},
					{
						"type": "context",
						"elements": []map[string]string{
							{"type": "mrkdwn", "text": "Branch: `" + job.Branch + "` | Version: `" + job.TerraformVersion + "`"},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[Slack] marshal error: %v", err)
		return
	}
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[Slack] send error: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[Slack] sent %q (HTTP %d)", title, resp.StatusCode)
}

// --- Public notification helpers ---

// notifySlackApproved fires at the start of terraformApply / terraformDestroy,
// signalling that the approval gate was passed and the operation is beginning.
func (p *JobProcessor) notifySlackApproved(job *model.TerraformJob) {
	url, ok := p.slackEnabled(job)
	if !ok {
		return
	}
	title := ":white_check_mark: *Approved — Applying Changes*"
	if job.Type == "terraformDestroy" {
		title = ":white_check_mark: *Approved — Destroying Resources*"
	}
	p.slackSend(url, "#1463fb", title, job)
}

// notifySlackPlanPending fires when a plan detects changes (exit code 2)
// and the job moves to the approval-pending state.
func (p *JobProcessor) notifySlackPlanPending(job *model.TerraformJob) {
	url, ok := p.slackEnabled(job)
	if !ok {
		return
	}
	title := ":hourglass_flowing_sand: *Plan Ready — Awaiting Approval*"
	if job.Type == "terraformPlanDestroy" {
		title = ":hourglass_flowing_sand: *Destroy Plan Ready — Awaiting Approval*"
	}
	p.slackSend(url, "#eda509", title, job)
}

// notifySlackPlanNoChanges fires when a plan detects no changes (exit code 0).
func (p *JobProcessor) notifySlackPlanNoChanges(job *model.TerraformJob) {
	url, ok := p.slackEnabled(job)
	if !ok {
		return
	}
	p.slackSend(url, "#1463fb", ":zzz: *No Changes Detected*", job)
}

// notifySlackSuccess fires when terraformApply or terraformDestroy completes successfully.
func (p *JobProcessor) notifySlackSuccess(job *model.TerraformJob) {
	url, ok := p.slackEnabled(job)
	if !ok {
		return
	}
	title := ":rocket: *Terraform Apply Completed*"
	if job.Type == "terraformDestroy" {
		title = ":white_check_mark: *Terraform Destroy Completed*"
	}
	p.slackSend(url, "#36a64f", title, job)
}

// notifySlackOnFailure fires when any terraform step fails.
// Activated by SLACK_WEBHOOK_URL alone (no ENABLE_SLACK_NOTIFICATIONS guard)
// so failures are always reported if a webhook is configured.
func (p *JobProcessor) notifySlackOnFailure(job *model.TerraformJob) {
	webhookURL := job.EnvironmentVariables["SLACK_WEBHOOK_URL"]
	if webhookURL == "" {
		return
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
	p.slackSend(webhookURL, "#cc0000", title, job)
}
