package adr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// GitHubClient provides access to the GitHub API for posting comments
// and creating issues.
type GitHubClient struct {
	config     *Config
	httpClient *http.Client
	logger     *slog.Logger
	token      string
	baseURL    string
}

// NewGitHubClient creates a new GitHub API client.
func NewGitHubClient(config *Config, token string, logger *slog.Logger) *GitHubClient {
	return &GitHubClient{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger:  logger.With("component", "github-client"),
		token:   token,
		baseURL: "https://api.github.com",
	}
}

// PostPRComment posts a comment on a GitHub pull request.
func (c *GitHubClient) PostPRComment(ctx context.Context, repoFullName string, prNumber int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.baseURL, repoFullName, prNumber)

	payload := map[string]string{
		"body": body,
	}

	return c.doPost(ctx, url, payload)
}

// ListPRComments lists all comments on a GitHub pull request.
func (c *GitHubClient) ListPRComments(ctx context.Context, repoFullName string, prNumber int) ([]PRComment, error) {
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.baseURL, repoFullName, prNumber)

	var comments []PRComment
	if err := c.doGet(ctx, url, &comments); err != nil {
		return nil, err
	}

	return comments, nil
}

// GetPRDiff retrieves the diff for a pull request.
func (c *GitHubClient) GetPRDiff(ctx context.Context, repoFullName string, prNumber int) (*PRDiff, error) {
	// First get the list of changed files
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/files", c.baseURL, repoFullName, prNumber)

	var files []DiffFile
	if err := c.doGet(ctx, url, &files); err != nil {
		return nil, fmt.Errorf("get PR files: %w", err)
	}

	diff := &PRDiff{
		Files:      files,
		TotalFiles: len(files),
	}

	for _, f := range files {
		diff.TotalAdd += f.Additions
		diff.TotalDelete += f.Deletions
	}

	return diff, nil
}

// CreateIssue creates a new GitHub issue.
func (c *GitHubClient) CreateIssue(ctx context.Context, repoFullName string, title string, body string) error {
	url := fmt.Sprintf("%s/repos/%s/issues", c.baseURL, repoFullName)

	payload := map[string]string{
		"title": title,
		"body":  body,
	}

	return c.doPost(ctx, url, payload)
}

// doPost performs a POST request to the GitHub API.
func (c *GitHubClient) doPost(ctx context.Context, url string, payload any) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// doGet performs a GET request to the GitHub API.
func (c *GitHubClient) doGet(ctx context.Context, url string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// setHeaders sets the required headers for GitHub API requests.
func (c *GitHubClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	req.Header.Set("User-Agent", "governor-adr-pipeline/1.0")
}

// ValidateWebhookPayload validates the structure of a webhook payload.
func ValidateWebhookPayload(payload []byte) (*WebhookEvent, error) {
	var event WebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("invalid webhook payload: %w", err)
	}

	// Validate required fields
	if event.Action == "" {
		return nil, fmt.Errorf("missing action field")
	}

	return &event, nil
}

// IsArchitecturalChange checks if a PR contains architectural changes.
func IsArchitecturalChange(files []DiffFile, architecturalPaths []string) bool {
	for _, file := range files {
		for _, prefix := range architecturalPaths {
			if strings.HasPrefix(file.Filename, prefix) {
				return true
			}
		}
	}
	return false
}

// CalculateMeaningfulLines calculates the number of meaningful lines changed.
func CalculateMeaningfulLines(files []DiffFile) int {
	total := 0
	for _, file := range files {
		total += file.Additions + file.Deletions
	}
	return total
}
