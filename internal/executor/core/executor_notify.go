package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ilkerispir/terrakubed/internal/model"
)

// PlanSummary holds the resource change counts from a terraform plan output.
type PlanSummary struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
	Replace int `json:"replace"`
}

// parsePlanSummary extracts add/change/destroy counts from terraform plan text output.
// Looks for the summary line: "Plan: X to add, Y to change, Z to destroy."
// Strips ANSI escape sequences first (terraform outputs colors when -no-color is not set).
// Returns nil if the line is not found (e.g. no changes or parse error).
func parsePlanSummary(output string) *PlanSummary {
	// Strip ANSI color codes: ESC [ ... m
	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	clean := ansiRe.ReplaceAllString(output, "")

	re := regexp.MustCompile(`Plan:\s+(\d+) to add,\s*(\d+) to change,\s*(\d+) to destroy`)
	m := re.FindStringSubmatch(clean)
	if len(m) != 4 {
		return nil
	}
	add, _ := strconv.Atoi(m[1])
	change, _ := strconv.Atoi(m[2])
	destroy, _ := strconv.Atoi(m[3])
	return &PlanSummary{Add: add, Change: change, Destroy: destroy}
}

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

// slackSend builds and POSTs a Slack attachment message.
// Pass a non-nil summary to include a Plan Summary block (plan notifications only).
func (p *JobProcessor) slackSend(webhookURL, color, title string, job *model.TerraformJob, summary *PlanSummary) {
	// UI URL priority:
	// 1. TerrakubeUiURL / TERRAKUBE_UI_URL on the executor deployment (explicit)
	// 2. Org-level TERRAKUBE_UI_URL env var (backward compat)
	// 3. AzBuilderApiUrl — same host usually serves the UI; avoids needing a separate env var
	uiURL := p.Config.TerrakubeUiURL
	if uiURL == "" {
		uiURL = job.EnvironmentVariables["TERRAKUBE_UI_URL"]
	}
	if uiURL == "" {
		uiURL = p.Config.AzBuilderApiUrl
	}

	wsName := job.EnvironmentVariables["workspaceName"]
	if wsName == "" {
		wsName = job.WorkspaceId
	}

	wsText := wsName

	// Determine the run URL:
	//   - Explicit UI URL → direct link: {uiURL}/organizations/{orgId}/workspaces/{wsId}/runs/{jobId}
	//   - API URL fallback  → redirect: {apiURL}/app/{orgId}/{wsId}/runs/{jobId}
	//     (RedirectController on the API resolves org/ws from jobId and redirects to the UI)
	explicitUI := p.Config.TerrakubeUiURL
	if explicitUI == "" {
		explicitUI = job.EnvironmentVariables["TERRAKUBE_UI_URL"]
	}

	var runURL string
	if explicitUI != "" {
		base := strings.TrimRight(explicitUI, "/")
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "https://" + base
		}
		runURL = fmt.Sprintf("%s/organizations/%s/workspaces/%s/runs/%s",
			base, job.OrganizationId, job.WorkspaceId, job.JobId)
	} else if uiURL != "" {
		// uiURL is AzBuilderApiUrl here — use the /app/ redirect endpoint
		base := strings.TrimRight(uiURL, "/")
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "https://" + base
		}
		runURL = fmt.Sprintf("%s/app/%s/%s/runs/%s",
			base, job.OrganizationId, job.WorkspaceId, job.JobId)
	}

	if runURL != "" {
		wsText = fmt.Sprintf("<%s|%s>", runURL, wsName)
	}

	blocks := []map[string]interface{}{
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
	}

	// Append plan summary block when present
	if summary != nil {
		summaryText := fmt.Sprintf(
			"*Plan Summary*\n:seedling: Created: *%d*     :hammer_and_wrench: Updated: *%d*     :x: Deleted: *%d*",
			summary.Add, summary.Change, summary.Destroy,
		)
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": summaryText},
		})
	}

	blocks = append(blocks,
		map[string]interface{}{"type": "divider"},
		map[string]interface{}{
			"type": "context",
			"elements": []map[string]string{
				{"type": "mrkdwn", "text": "Branch: `" + job.Branch + "` | Version: `" + job.TerraformVersion + "`"},
			},
		},
	)

	msg := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":  color,
				"blocks": blocks,
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
	p.slackSend(url, "#1463fb", title, job, nil)
}

// notifySlackPlanPending fires when a plan detects changes (exit code 2)
// and the job moves to the approval-pending state.
func (p *JobProcessor) notifySlackPlanPending(job *model.TerraformJob, summary *PlanSummary) {
	url, ok := p.slackEnabled(job)
	if !ok {
		return
	}
	title := ":hourglass_flowing_sand: *Plan Ready — Awaiting Approval*"
	if job.Type == "terraformPlanDestroy" {
		title = ":hourglass_flowing_sand: *Destroy Plan Ready — Awaiting Approval*"
	}
	p.slackSend(url, "#eda509", title, job, summary)
}

// notifySlackPlanNoChanges fires when a plan detects no changes (exit code 0).
func (p *JobProcessor) notifySlackPlanNoChanges(job *model.TerraformJob) {
	url, ok := p.slackEnabled(job)
	if !ok {
		return
	}
	p.slackSend(url, "#1463fb", ":zzz: *No Changes Detected*", job, nil)
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
	p.slackSend(url, "#36a64f", title, job, nil)
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
	p.slackSend(webhookURL, "#cc0000", title, job, nil)
}
