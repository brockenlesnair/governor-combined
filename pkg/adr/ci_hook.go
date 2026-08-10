package adr

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// CIHookHandler handles GitHub App webhook events for PR merged events.
type CIHookHandler struct {
	config   *Config
	client   AdrMcpClient
	logger   *slog.Logger
	ghClient *GitHubClient
}

// NewCIHookHandler creates a new CI hook handler.
func NewCIHookHandler(config *Config, client AdrMcpClient, logger *slog.Logger) *CIHookHandler {
	return &CIHookHandler{
		config: config,
		client: client,
		logger: logger.With("component", "ci-hook"),
	}
}

// SetGitHubClient sets the GitHub API client for posting comments.
func (h *CIHookHandler) SetGitHubClient(client *GitHubClient) {
	h.ghClient = client
}

// HandleWebhook processes incoming GitHub webhook events.
func (h *CIHookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify webhook signature
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		h.logger.Warn("missing webhook signature")
		http.Error(w, "missing signature", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("read request body", "err", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if !h.verifySignature(body, sigHeader) {
		h.logger.Warn("invalid webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse event
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("parse webhook event", "err", err)
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Only process PR merged events
	if event.PullRequest == nil || event.Action != "closed" {
		h.logger.Debug("ignoring event",
			"action", event.Action,
			"has_pr", event.PullRequest != nil,
		)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Check if PR was actually merged (not just closed)
	if event.PullRequest.MergeCommit == nil {
		h.logger.Debug("PR closed without merge",
			"pr_number", event.PullRequest.Number,
		)
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Info("processing merged PR",
		"pr_number", event.PullRequest.Number,
		"pr_title", event.PullRequest.Title,
		"repo", event.Repository.FullName,
		"merge_commit", *event.PullRequest.MergeCommit,
	)

	// Process asynchronously
	go h.processMergedPR(r.Context(), event)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "accepted",
		"pr":     fmt.Sprintf("#%d", event.PullRequest.Number),
	})
}

// verifySignature validates the HMAC-SHA256 webhook signature.
func (h *CIHookHandler) verifySignature(payload []byte, sigHeader string) bool {
	// Remove "sha256=" prefix
	sigHex := strings.TrimPrefix(sigHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(h.config.GitHub.WebhookSecret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	expectedHex := hex.EncodeToString(expectedMAC)
	return hmac.Equal([]byte(sigHex), []byte(expectedHex))
}

// processMergedPR handles a merged PR event.
func (h *CIHookHandler) processMergedPR(ctx context.Context, event WebhookEvent) {
	pr := event.PullRequest
	repo := event.Repository

	logger := h.logger.With(
		"pr_number", pr.Number,
		"repo", repo.FullName,
	)

	// Get PR diff
	diff, err := h.getPRDiff(ctx, repo.FullName, pr.Number)
	if err != nil {
		logger.Error("failed to get PR diff", "err", err)
		return
	}

	// Filter for architectural changes
	archDiff := h.filterArchitecturalChanges(diff)
	if archDiff == nil || len(archDiff.Files) == 0 {
		logger.Info("no architectural changes detected, skipping ADR evaluation")
		return
	}

	// Trivial change detection
	if h.isTrivialChange(archDiff) {
		logger.Info("change is trivial, skipping ADR evaluation",
			"total_add", archDiff.TotalAdd,
			"total_delete", archDiff.TotalDelete,
		)
		return
	}

	logger.Info("architectural changes detected",
		"files", len(archDiff.Files),
		"total_add", archDiff.TotalAdd,
		"total_delete", archDiff.TotalDelete,
	)

	// Resolve project from repo
	_, _ = h.resolveProject(repo.FullName)

	// Preview-first: create ADR with previewOnly=true
	result, err := h.client.CallTool(ctx, "create_adr", map[string]any{
		"title":       pr.Title,
		"status":      "proposed",
		"previewOnly": true,
		"diff":        formatDiffForADR(archDiff),
		"pr_number":   pr.Number,
		"repo":        repo.FullName,
	})
	if err != nil {
		logger.Error("failed to create ADR preview", "err", err)
		return
	}

	// Extract diff and post as PR comment
	diffStr, _ := result["diff"].(string)
	if diffStr != "" {
		filePaths, _ := result["file_paths"].([]string)
		adrPath, _ := result["adr_path"].(string)

		previewComment := &PreviewComment{
			ADRTitle:    pr.Title,
			ADRStatus:   "proposed",
			PreviewDiff: diffStr,
			FilePaths:   filePaths,
		}

		if err := h.postPRComment(ctx, repo.FullName, pr.Number, previewComment); err != nil {
			logger.Error("failed to post PR comment", "err", err)
			return
		}

		logger.Info("ADR preview posted as PR comment",
			"adr_path", adrPath,
		)
	}
}

// getPRDiff retrieves the diff for a pull request.
func (h *CIHookHandler) getPRDiff(ctx context.Context, repoFullName string, prNumber int) (*PRDiff, error) {
	if h.ghClient == nil {
		return nil, fmt.Errorf("GitHub client not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return h.ghClient.GetPRDiff(ctx, repoFullName, prNumber)
}

// filterArchitecturalChanges filters diff files to only architectural paths.
func (h *CIHookHandler) filterArchitecturalChanges(diff *PRDiff) *PRDiff {
	if diff == nil {
		return nil
	}

	var filtered []DiffFile
	for _, file := range diff.Files {
		if h.isArchitecturalPath(file.Filename) {
			filtered = append(filtered, file)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	result := &PRDiff{
		Files:      filtered,
		TotalFiles: len(filtered),
	}

	for _, f := range filtered {
		result.TotalAdd += f.Additions
		result.TotalDelete += f.Deletions
	}

	return result
}

// isArchitecturalPath checks if a file path matches architectural patterns.
func (h *CIHookHandler) isArchitecturalPath(path string) bool {
	for _, prefix := range h.config.DiffFilter.ArchitecturalPaths {
		if strings.HasPrefix(path, prefix) || path == prefix {
			return true
		}
		// Check for exact filename match (e.g., go.mod, package.json)
		if !strings.HasSuffix(prefix, "/") && path == prefix {
			return true
		}
	}
	return false
}

// isTrivialChange checks if a change has fewer than the minimum meaningful lines.
func (h *CIHookHandler) isTrivialChange(diff *PRDiff) bool {
	if diff == nil {
		return true
	}

	meaningfulLines := diff.TotalAdd + diff.TotalDelete
	return meaningfulLines < h.config.DiffFilter.MinMeaningfulLines
}

// resolveProject determines the server project for a repository.
func (h *CIHookHandler) resolveProject(repoFullName string) (ServerProject, error) {
	// In production, this would look up project configuration
	// For now, derive project ID from repo name
	projectID := strings.ReplaceAll(repoFullName, "/", "-")

	return ServerProject{
		ProjectID: projectID,
		AdrRoot:   h.config.AdrRoot,
		RepoRoot:  h.config.RepoRoot,
	}, nil
}

// postPRComment posts a comment on a GitHub PR.
func (h *CIHookHandler) postPRComment(ctx context.Context, repoFullName string, prNumber int, comment *PreviewComment) error {
	if h.ghClient == nil {
		return fmt.Errorf("GitHub client not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	body := comment.String()
	return h.ghClient.PostPRComment(ctx, repoFullName, prNumber, body)
}

// formatDiffForADR converts a PRDiff to a format suitable for AdrMcp.
func formatDiffForADR(diff *PRDiff) string {
	if diff == nil {
		return ""
	}

	var sb strings.Builder
	for _, file := range diff.Files {
		sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", file.Filename, file.Filename))
		sb.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", file.Deletions, file.Additions))
	}
	return sb.String()
}
