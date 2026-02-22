package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type TerrakubeClient struct {
	ApiUrl     string
	Token      string
	HttpClient *http.Client
}

func NewTerrakubeClient(apiUrl string, token string) *TerrakubeClient {
	return &TerrakubeClient{
		ApiUrl: apiUrl,
		Token:  token,
		HttpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *TerrakubeClient) UpdateJobStatus(orgId, jobId string, status string, output string) error {
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "job",
			"id":   jobId,
			"attributes": map[string]interface{}{
				"status": status,
				"output": output,
			},
		},
	}
	return c.patch(fmt.Sprintf("/api/v1/organization/%s/job/%s", orgId, jobId), payload)
}

func (c *TerrakubeClient) UpdateStepStatus(orgId, jobId, stepId string, status string, output string) error {
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "step",
			"id":   stepId,
			"attributes": map[string]interface{}{
				"status": status,
				"output": output,
			},
		},
	}
	return c.patch(fmt.Sprintf("/api/v1/organization/%s/job/%s/step/%s", orgId, jobId, stepId), payload)
}

// UpdateJobCommitId updates the job's commit ID.
func (c *TerrakubeClient) UpdateJobCommitId(orgId, jobId, commitId string) error {
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "job",
			"id":   jobId,
			"attributes": map[string]interface{}{
				"commitId": commitId,
			},
		},
	}
	return c.patch(fmt.Sprintf("/api/v1/organization/%s/job/%s", orgId, jobId), payload)
}

// CreateHistory creates a workspace history record after apply/destroy.
func (c *TerrakubeClient) CreateHistory(orgId, workspaceId, stateURL string) error {
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "history",
			"attributes": map[string]interface{}{
				"output": stateURL,
			},
		},
	}
	return c.post(fmt.Sprintf("/api/v1/organization/%s/workspace/%s/history", orgId, workspaceId), payload)
}

func (c *TerrakubeClient) patch(path string, payload interface{}) error {
	return c.doRequest("PATCH", path, payload)
}

func (c *TerrakubeClient) post(path string, payload interface{}) error {
	return c.doRequest("POST", path, payload)
}

func (c *TerrakubeClient) doRequest(method, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.ApiUrl, path), bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/vnd.api+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	return nil
}
