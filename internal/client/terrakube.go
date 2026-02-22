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

// UpdateJobStatus updates the job status in Terrakube API
func (c *TerrakubeClient) UpdateJobStatus(orgId, jobId string, status string, output string) error {
	// Simplified payload for now
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

// UpdateStepStatus updates the step status
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

func (c *TerrakubeClient) patch(path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s%s", c.ApiUrl, path), bytes.NewBuffer(body))
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
