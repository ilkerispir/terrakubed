package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	// Ensure the base URL ends with /graphql/api/v1
	graphqlURL := baseURL
	if !strings.HasSuffix(baseURL, "/graphql/api/v1") {
		// Clean up any trailing slashes or /graphql suffix
		base := strings.TrimRight(baseURL, "/")
		base = strings.TrimSuffix(base, "/graphql")
		graphqlURL = fmt.Sprintf("%s/graphql/api/v1", base)
	}

	return &Client{
		BaseURL: graphqlURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type GraphQLRequest struct {
	Query string `json:"query"`
}

type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) ExecuteQuery(query string, variables map[string]interface{}) ([]byte, error) {
	requestBody, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	req, err := http.NewRequest("POST", c.BaseURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		// Generate JWT token
		// Terrakube API expects BASE64URL without padding (Java's Decoders.BASE64URL returns standard byte[])
		secret, err := base64.RawURLEncoding.DecodeString(c.Token)
		if err != nil {
			// Fallback to std encoding just in case
			secret, err = base64.StdEncoding.DecodeString(c.Token)
		}
		if err == nil {
			claims := jwt.MapClaims{
				"iss":            "TerrakubeInternal",
				"sub":            "TerrakubeInternal (TOKEN)",
				"aud":            "TerrakubeInternal",
				"email":          "no-reply@terrakube.io",
				"email_verified": true,
				"name":           "TerrakubeInternal Client",
				"iat":            time.Now().Unix(),
				"exp":            time.Now().Add(30 * 24 * time.Hour).Unix(),
			}

			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			token.Header["typ"] = "JWT"
			signedToken, err := token.SignedString(secret)
			if err == nil {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", signedToken))
			}
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(body))
	}

	var graphQLResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphQLResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", graphQLResp.Errors[0].Message)
	}

	return graphQLResp.Data, nil
}

func (c *Client) GetVcsToken(orgId, vcsId string) (string, error) {
	// REST API: GET /api/v1/organization/{orgId}/vcs/{vcsId}
	base := strings.TrimSuffix(c.BaseURL, "/graphql/api/v1")
	restURL := fmt.Sprintf("%s/api/v1/organization/%s/vcs/%s", base, orgId, vcsId)

	req, err := http.NewRequest("GET", restURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if c.Token != "" {
		secret, err := base64.RawURLEncoding.DecodeString(c.Token)
		if err != nil {
			secret, _ = base64.StdEncoding.DecodeString(c.Token)
		}
		if len(secret) > 0 {
			claims := jwt.MapClaims{
				"iss":            "TerrakubeInternal",
				"sub":            "TerrakubeInternal (TOKEN)",
				"aud":            "TerrakubeInternal",
				"email":          "no-reply@terrakube.io",
				"email_verified": true,
				"name":           "TerrakubeInternal Client",
				"iat":            time.Now().Unix(),
				"exp":            time.Now().Add(30 * 24 * time.Hour).Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			token.Header["typ"] = "JWT"
			signedToken, err := token.SignedString(secret)
			if err == nil {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", signedToken))
			}
		}
	}

	req.Header.Set("Content-Type", "application/vnd.api+json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Attributes struct {
				AccessToken string `json:"accessToken"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data.Attributes.AccessToken, nil
}
