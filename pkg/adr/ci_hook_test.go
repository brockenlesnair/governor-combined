package adr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"
)

func testConfig() *Config {
	cfg := &Config{}
	cfg.GitHub.WebhookSecret = "test-webhook-secret"
	cfg.DiffFilter.ArchitecturalPaths = []string{
		"pkg/", "cmd/", "internal/",
		"go.mod", "go.sum", "package.json",
		"Dockerfile", "docker-compose.yml",
	}
	cfg.DiffFilter.MinMeaningfulLines = 10
	cfg.AdrRoot = "/tmp/adrs"
	cfg.RepoRoot = "/tmp/repos"
	cfg.Preview.CommentTag = "<!-- adr-preview -->"
	return cfg
}

func TestNewCIHookHandler(t *testing.T) {
	config := testConfig()
	client := NewGoStubAdrMcpClient(slog.Default())
	handler := NewCIHookHandler(config, client, slog.Default())

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.config != config {
		t.Error("config not set correctly")
	}
	if handler.client != client {
		t.Error("client not set correctly")
	}
}

func TestCIHookHandler_SetGitHubClient(t *testing.T) {
	handler := NewCIHookHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ghClient := NewGitHubClient(testConfig(), "token", slog.Default())
	handler.SetGitHubClient(ghClient)

	if handler.ghClient != ghClient {
		t.Error("GitHub client not set correctly")
	}
}

func TestCIHookHandler_VerifyWebhook(t *testing.T) {
	secret := "test-webhook-secret"
	config := testConfig()
	config.GitHub.WebhookSecret = secret

	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	payload := []byte(`{"action":"closed","pull_request":{"number":42}}`)

	// Compute expected HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !handler.verifySignature(payload, expectedSig) {
		t.Error("valid signature should be accepted")
	}
}

func TestCIHookHandler_VerifyWebhook_InvalidSignature(t *testing.T) {
	handler := NewCIHookHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	payload := []byte(`{"action":"closed","pull_request":{"number":42}}`)
	invalidSig := "sha256=0000000000000000000000000000000000000000000000000000000000000000"

	if handler.verifySignature(payload, invalidSig) {
		t.Error("invalid signature should be rejected")
	}
}

func TestCIHookHandler_VerifyWebhook_WrongSecret(t *testing.T) {
	config := testConfig()
	config.GitHub.WebhookSecret = "correct-secret"

	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	payload := []byte(`{"action":"closed","pull_request":{"number":42}}`)

	// Sign with wrong secret
	mac := hmac.New(sha256.New, []byte("wrong-secret"))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if handler.verifySignature(payload, sig) {
		t.Error("signature signed with wrong secret should be rejected")
	}
}

func TestCIHookHandler_IsArchitecturalPath(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	tests := []struct {
		path     string
		expected bool
	}{
		{"pkg/adr/ci_hook.go", true},
		{"cmd/server/main.go", true},
		{"internal/config/config.go", true},
		{"go.mod", true},
		{"go.sum", true},
		{"package.json", true},
		{"Dockerfile", true},
		{"docker-compose.yml", true},
		{"README.md", false},
		{"docs/architecture.md", false},
		{"CHANGELOG.md", false},
		{"LICENSE", false},
		{"test/test.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := handler.isArchitecturalPath(tt.path); got != tt.expected {
				t.Errorf("isArchitecturalPath(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestCIHookHandler_DetectArchitecturalChanges(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	diff := &PRDiff{
		Files: []DiffFile{
			{Filename: "pkg/adr/ci_hook.go", Additions: 50, Deletions: 10},
			{Filename: "go.mod", Additions: 1, Deletions: 0},
			{Filename: "cmd/server/main.go", Additions: 20, Deletions: 5},
		},
		TotalFiles: 3,
	}

	filtered := handler.filterArchitecturalChanges(diff)

	if filtered == nil {
		t.Fatal("expected non-nil filtered diff")
	}
	if len(filtered.Files) != 3 {
		t.Errorf("expected 3 architectural files, got %d", len(filtered.Files))
	}
	if filtered.TotalAdd != 71 {
		t.Errorf("expected 71 additions, got %d", filtered.TotalAdd)
	}
	if filtered.TotalDelete != 15 {
		t.Errorf("expected 15 deletions, got %d", filtered.TotalDelete)
	}
}

func TestCIHookHandler_DetectArchitecturalChanges_NonArchitectural(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	diff := &PRDiff{
		Files: []DiffFile{
			{Filename: "README.md", Additions: 20, Deletions: 0},
			{Filename: "docs/architecture.md", Additions: 50, Deletions: 0},
			{Filename: "CHANGELOG.md", Additions: 100, Deletions: 0},
		},
		TotalFiles: 3,
	}

	filtered := handler.filterArchitecturalChanges(diff)

	if filtered != nil {
		t.Error("expected nil for non-architectural changes")
	}
}

func TestCIHookHandler_FilterArchitecturalChanges_Nil(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	if handler.filterArchitecturalChanges(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestCIHookHandler_FilterArchitecturalChanges_Mixed(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	diff := &PRDiff{
		Files: []DiffFile{
			{Filename: "pkg/adr/ci_hook.go", Additions: 10, Deletions: 5},
			{Filename: "README.md", Additions: 20, Deletions: 0},
			{Filename: "go.mod", Additions: 1, Deletions: 0},
		},
		TotalFiles: 3,
	}

	filtered := handler.filterArchitecturalChanges(diff)

	if filtered == nil {
		t.Fatal("expected non-nil filtered diff")
	}
	if len(filtered.Files) != 2 {
		t.Errorf("expected 2 architectural files, got %d", len(filtered.Files))
	}
	if filtered.TotalAdd != 11 {
		t.Errorf("expected 11 additions, got %d", filtered.TotalAdd)
	}
}

func TestCIHookHandler_IsTrivialChange(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	// Trivial: less than MinMeaningfulLines (10)
	trivialDiff := &PRDiff{TotalAdd: 3, TotalDelete: 2}
	if !handler.isTrivialChange(trivialDiff) {
		t.Error("5 lines should be trivial")
	}

	// Not trivial: 10+ lines
	significantDiff := &PRDiff{TotalAdd: 8, TotalDelete: 5}
	if handler.isTrivialChange(significantDiff) {
		t.Error("13 lines should not be trivial")
	}

	// Nil diff is trivial
	if !handler.isTrivialChange(nil) {
		t.Error("nil diff should be trivial")
	}

	// Exactly at threshold
	atThreshold := &PRDiff{TotalAdd: 5, TotalDelete: 5}
	if handler.isTrivialChange(atThreshold) {
		t.Error("10 lines should not be trivial")
	}
}

func TestCIHookHandler_ResolveProject(t *testing.T) {
	config := testConfig()
	handler := NewCIHookHandler(config, NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	project, err := handler.resolveProject("owner/repo")
	if err != nil {
		t.Fatal(err)
	}

	if project.ProjectID != "owner-repo" {
		t.Errorf("expected project ID 'owner-repo', got %s", project.ProjectID)
	}
	if project.AdrRoot != "/tmp/adrs" {
		t.Errorf("expected ADR root '/tmp/adrs', got %s", project.AdrRoot)
	}
	if project.RepoRoot != "/tmp/repos" {
		t.Errorf("expected repo root '/tmp/repos', got %s", project.RepoRoot)
	}
}

func TestFormatDiffForADR(t *testing.T) {
	diff := &PRDiff{
		Files: []DiffFile{
			{Filename: "pkg/adr/ci_hook.go", Additions: 10, Deletions: 5},
			{Filename: "pkg/adr/types.go", Additions: 3, Deletions: 1},
		},
	}

	result := formatDiffForADR(diff)

	if !strings.Contains(result, "--- a/pkg/adr/ci_hook.go") {
		t.Error("expected file header for ci_hook.go")
	}
	if !strings.Contains(result, "+++ b/pkg/adr/ci_hook.go") {
		t.Error("expected +++ header for ci_hook.go")
	}
	if !strings.Contains(result, "--- a/pkg/adr/types.go") {
		t.Error("expected file header for types.go")
	}
	if !strings.Contains(result, "@@ -5 +10 @@") {
		t.Error("expected hunk header")
	}
}

func TestFormatDiffForADR_Nil(t *testing.T) {
	result := formatDiffForADR(nil)
	if result != "" {
		t.Errorf("expected empty string for nil diff, got %q", result)
	}
}

func TestPreviewComment_String(t *testing.T) {
	pc := &PreviewComment{
		ADRTitle:    "Use PostgreSQL for persistence",
		ADRStatus:   "proposed",
		PreviewDiff: "+ CREATE TABLE users (id INT PRIMARY KEY);",
		FilePaths:   []string{"migrations/001.sql", "pkg/db/client.go"},
	}

	result := pc.String()

	if !strings.Contains(result, "ADR Proposal: Use PostgreSQL for persistence") {
		t.Error("expected ADR title in output")
	}
	if !strings.Contains(result, "proposed") {
		t.Error("expected status in output")
	}
	if !strings.Contains(result, "+ CREATE TABLE users") {
		t.Error("expected diff in output")
	}
	if !strings.Contains(result, "`migrations/001.sql`") {
		t.Error("expected formatted file paths")
	}
	if !strings.Contains(result, "Generated by AdrMcp Pipeline") {
		t.Error("expected generator tag")
	}
}

func TestFormatFilePaths(t *testing.T) {
	// Empty paths
	result := formatFilePaths(nil)
	if result != "none" {
		t.Errorf("expected 'none' for nil paths, got %q", result)
	}

	// Single path
	result = formatFilePaths([]string{"file.go"})
	if result != "`file.go`" {
		t.Errorf("expected '`file.go`', got %q", result)
	}

	// Multiple paths
	result = formatFilePaths([]string{"a.go", "b.go", "c.go"})
	if result != "`a.go`, `b.go`, `c.go`" {
		t.Errorf("expected comma-separated paths, got %q", result)
	}
}

func TestFormatActions(t *testing.T) {
	pc := &PreviewComment{}
	result := formatActions(pc)

	if !strings.Contains(result, "Accept:") {
		t.Error("expected Accept action")
	}
	if !strings.Contains(result, "Reject:") {
		t.Error("expected Reject action")
	}
	if !strings.Contains(result, "Update:") {
		t.Error("expected Update action")
	}
}
