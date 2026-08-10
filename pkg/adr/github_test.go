package adr

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewGitHubClient(t *testing.T) {
	config := testConfig()
	client := NewGitHubClient(config, "test-token", slog.Default())

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.token != "test-token" {
		t.Errorf("expected token 'test-token', got %s", client.token)
	}
	if client.config != config {
		t.Error("config not set correctly")
	}
	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}

func TestGitHubClient_ListPRComments(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/issues/42/comments") {
			t.Errorf("unexpected URL path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]PRComment{
			{ID: 1, Body: "Looks good!", HTML: "https://github.com/test/comment/1"},
			{ID: 2, Body: "Please review", HTML: "https://github.com/test/comment/2"},
		})
	}))
	defer ts.Close()

	client := &GitHubClient{
		config:     testConfig(),
		httpClient: ts.Client(),
		logger:     slog.Default(),
		token:      "test-token",
		baseURL:    ts.URL,
	}

	ctx := context.Background()
	comments, err := client.ListPRComments(ctx, "owner/repo", 42)
	if err != nil {
		t.Fatal(err)
	}

	if len(comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].Body != "Looks good!" {
		t.Errorf("expected 'Looks good!', got %q", comments[0].Body)
	}
}

func TestGitHubClient_ListPRComments_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]PRComment{})
	}))
	defer ts.Close()

	client := &GitHubClient{
		config:     testConfig(),
		httpClient: ts.Client(),
		logger:     slog.Default(),
		token:      "test-token",
		baseURL:    ts.URL,
	}

	ctx := context.Background()
	comments, err := client.ListPRComments(ctx, "owner/repo", 42)
	if err != nil {
		t.Fatal(err)
	}

	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestGitHubClient_ListPRComments_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"Internal Server Error"}`))
	}))
	defer ts.Close()

	client := &GitHubClient{
		config:     testConfig(),
		httpClient: ts.Client(),
		logger:     slog.Default(),
		token:      "test-token",
		baseURL:    ts.URL,
	}

	ctx := context.Background()
	_, err := client.ListPRComments(ctx, "owner/repo", 42)
	if err == nil {
		t.Error("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got: %v", err)
	}
}

func TestGitHubClient_PostPRComment(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		if payload["body"] != "test comment" {
			t.Errorf("expected body 'test comment', got %q", payload["body"])
		}

		// Check auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	client := &GitHubClient{
		config:     testConfig(),
		httpClient: ts.Client(),
		logger:     slog.Default(),
		token:      "test-token",
		baseURL:    ts.URL,
	}

	ctx := context.Background()
	err := client.PostPRComment(ctx, "owner/repo", 42, "test comment")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitHubClient_GetPRDiff(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pulls/42/files") {
			t.Errorf("unexpected URL path: %s", r.URL.Path)
		}

		files := []DiffFile{
			{Filename: "main.go", Status: "modified", Additions: 10, Deletions: 5, Changes: 15},
			{Filename: "test.go", Status: "added", Additions: 20, Deletions: 0, Changes: 20},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(files)
	}))
	defer ts.Close()

	client := &GitHubClient{
		config:     testConfig(),
		httpClient: ts.Client(),
		logger:     slog.Default(),
		token:      "test-token",
		baseURL:    ts.URL,
	}

	ctx := context.Background()
	diff, err := client.GetPRDiff(ctx, "owner/repo", 42)
	if err != nil {
		t.Fatal(err)
	}

	if diff.TotalFiles != 2 {
		t.Errorf("expected 2 files, got %d", diff.TotalFiles)
	}
	if diff.TotalAdd != 30 {
		t.Errorf("expected 30 additions, got %d", diff.TotalAdd)
	}
	if diff.TotalDelete != 5 {
		t.Errorf("expected 5 deletions, got %d", diff.TotalDelete)
	}
}

func TestGitHubClient_PostPRComment_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer ts.Close()

	client := &GitHubClient{
		config:     testConfig(),
		httpClient: ts.Client(),
		logger:     slog.Default(),
		token:      "test-token",
		baseURL:    ts.URL,
	}

	ctx := context.Background()
	err := client.PostPRComment(ctx, "owner/repo", 42, "test")
	if err == nil {
		t.Error("expected error for 404 status")
	}
}

func TestValidateWebhookPayload(t *testing.T) {
	payload := []byte(`{"action":"closed","pull_request":{"number":42,"title":"Test PR"}}`)

	event, err := ValidateWebhookPayload(payload)
	if err != nil {
		t.Fatal(err)
	}

	if event.Action != "closed" {
		t.Errorf("expected action 'closed', got %s", event.Action)
	}
	if event.PullRequest == nil {
		t.Fatal("expected non-nil PullRequest")
	}
	if event.PullRequest.Number != 42 {
		t.Errorf("expected PR number 42, got %d", event.PullRequest.Number)
	}
	if event.PullRequest.Title != "Test PR" {
		t.Errorf("expected title 'Test PR', got %s", event.PullRequest.Title)
	}
}

func TestValidateWebhookPayload_MissingAction(t *testing.T) {
	payload := []byte(`{"pull_request":{"number":42}}`)

	_, err := ValidateWebhookPayload(payload)
	if err == nil {
		t.Error("expected error for missing action")
	}
	if !strings.Contains(err.Error(), "missing action") {
		t.Errorf("expected 'missing action' error, got: %v", err)
	}
}

func TestValidateWebhookPayload_InvalidJSON(t *testing.T) {
	_, err := ValidateWebhookPayload([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestIsArchitecturalChange(t *testing.T) {
	files := []DiffFile{
		{Filename: "pkg/adr/ci_hook.go"},
		{Filename: "README.md"},
		{Filename: "go.mod"},
	}

	paths := []string{"pkg/", "go.mod"}
	if !IsArchitecturalChange(files, paths) {
		t.Error("should detect architectural changes")
	}

	nonArchPaths := []string{"src/"}
	if IsArchitecturalChange(files, nonArchPaths) {
		t.Error("should not detect non-architectural changes")
	}
}

func TestIsArchitecturalChange_Empty(t *testing.T) {
	if IsArchitecturalChange(nil, []string{"pkg/"}) {
		t.Error("empty files should not be architectural")
	}
}

func TestCalculateMeaningfulLines(t *testing.T) {
	files := []DiffFile{
		{Additions: 10, Deletions: 5},
		{Additions: 3, Deletions: 1},
	}

	total := CalculateMeaningfulLines(files)
	if total != 19 {
		t.Errorf("expected 19, got %d", total)
	}
}

func TestCalculateMeaningfulLines_Empty(t *testing.T) {
	total := CalculateMeaningfulLines(nil)
	if total != 0 {
		t.Errorf("expected 0 for empty files, got %d", total)
	}
}

func TestGitHubClient_SetHeaders(t *testing.T) {
	client := NewGitHubClient(testConfig(), "my-token", slog.Default())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	client.setHeaders(req)

	if req.Header.Get("Accept") != "application/vnd.github.v3+json" {
		t.Error("expected GitHub accept header")
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Error("expected content-type header")
	}
	if req.Header.Get("Authorization") != "Bearer my-token" {
		t.Error("expected authorization header")
	}
	if !strings.Contains(req.Header.Get("User-Agent"), "governor-adr-pipeline") {
		t.Error("expected user-agent header")
	}
}

func TestGitHubClient_SetHeaders_NoToken(t *testing.T) {
	client := NewGitHubClient(testConfig(), "", slog.Default())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	client.setHeaders(req)

	if req.Header.Get("Authorization") != "" {
		t.Error("should not set authorization when token is empty")
	}
}
