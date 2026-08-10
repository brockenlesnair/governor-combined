package adr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// CommandHandler processes /adr slash commands from PR comments.
type CommandHandler struct {
	config   *Config
	client   AdrMcpClient
	logger   *slog.Logger
	ghClient *GitHubClient
}

// NewCommandHandler creates a new command handler.
func NewCommandHandler(config *Config, client AdrMcpClient, logger *slog.Logger) *CommandHandler {
	return &CommandHandler{
		config: config,
		client: client,
		logger: logger.With("component", "command-handler"),
	}
}

// SetGitHubClient sets the GitHub API client for posting comments.
func (h *CommandHandler) SetGitHubClient(client *GitHubClient) {
	h.ghClient = client
}

// HandleComment processes a PR comment for slash commands.
func (h *CommandHandler) HandleComment(ctx context.Context, repoFullName string, prNumber int, commentBody string, author string) error {
	// Parse slash command
	cmd := ParseSlashCommand(commentBody)
	if cmd == nil {
		// Not a slash command, ignore
		return nil
	}

	logger := h.logger.With(
		"repo", repoFullName,
		"pr_number", prNumber,
		"author", author,
		"action", cmd.Action,
	)

	logger.Info("processing slash command")

	// Resolve project
	_, _ = h.resolveProject(repoFullName)

	// Dispatch command
	switch cmd.Action {
	case "accept":
		return h.handleAccept(ctx, h.client, repoFullName, prNumber, cmd, logger)
	case "reject":
		return h.handleReject(ctx, repoFullName, prNumber, cmd, logger)
	case "update":
		return h.handleUpdate(ctx, h.client, repoFullName, prNumber, cmd, logger)
	default:
		logger.Warn("unknown action", "action", cmd.Action)
		return h.postHelpComment(ctx, repoFullName, prNumber)
	}
}

// ParseSlashCommand parses a comment body for /adr commands.
func ParseSlashCommand(body string) *SlashCommand {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "/adr") {
		return nil
	}

	// Extract the rest after /adr
	rest := strings.TrimSpace(strings.TrimPrefix(body, "/adr"))
	if rest == "" {
		return &SlashCommand{
			Action: "help",
			Raw:    body,
		}
	}

	// Split into parts
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return &SlashCommand{
			Action: "help",
			Raw:    body,
		}
	}

	action := strings.ToLower(parts[0])
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	// Validate action
	validActions := map[string]bool{
		"accept": true,
		"reject": true,
		"update": true,
		"help":   true,
	}

	if !validActions[action] {
		return &SlashCommand{
			Action: "help",
			Raw:    body,
		}
	}

	return &SlashCommand{
		Action: action,
		Args:   args,
		Raw:    body,
	}
}

// handleAccept processes /adr accept command.
func (h *CommandHandler) handleAccept(ctx context.Context, client AdrMcpClient, repoFullName string, prNumber int, cmd *SlashCommand, logger *slog.Logger) error {
	logger.Info("accepting ADR proposal")

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Create ADR with previewOnly=false (actually create it)
	result, err := client.CallTool(ctx, "create_adr", map[string]any{
		"previewOnly": false,
		"pr_number":   prNumber,
		"repo":        repoFullName,
	})
	if err != nil {
		return fmt.Errorf("create ADR: %w", err)
	}

	// Post success comment
	adrPath, _ := result["adr_path"].(string)
	message, _ := result["message"].(string)
	comment := fmt.Sprintf("## ADR Accepted\n\nADR created successfully: `%s`\n\n%s",
		adrPath,
		message,
	)

	if err := h.postComment(ctx, repoFullName, prNumber, comment); err != nil {
		logger.Error("failed to post acceptance comment", "err", err)
		return fmt.Errorf("post comment: %w", err)
	}

	logger.Info("ADR accepted and created",
		"adr_path", adrPath,
	)
	return nil
}

// handleReject processes /adr reject command.
func (h *CommandHandler) handleReject(ctx context.Context, repoFullName string, prNumber int, cmd *SlashCommand, logger *slog.Logger) error {
	reason := "No reason provided"
	if len(cmd.Args) > 0 {
		reason = strings.Join(cmd.Args, " ")
	}

	logger.Info("rejecting ADR proposal", "reason", reason)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Post rejection comment
	comment := fmt.Sprintf("## ADR Rejected\n\n**Reason:** %s\n\nThis proposal will not be created as an ADR.",
		reason,
	)

	if err := h.postComment(ctx, repoFullName, prNumber, comment); err != nil {
		logger.Error("failed to post rejection comment", "err", err)
		return fmt.Errorf("post comment: %w", err)
	}

	logger.Info("ADR proposal rejected")
	return nil
}

// handleUpdate processes /adr update command.
func (h *CommandHandler) handleUpdate(ctx context.Context, client AdrMcpClient, repoFullName string, prNumber int, cmd *SlashCommand, logger *slog.Logger) error {
	changes := ""
	if len(cmd.Args) > 0 {
		changes = strings.Join(cmd.Args, " ")
	}

	logger.Info("updating ADR proposal", "changes", changes)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Update ADR with previewOnly=true (preview only)
	result, err := client.CallTool(ctx, "update_adr", map[string]any{
		"previewOnly": true,
		"pr_number":   prNumber,
		"repo":        repoFullName,
		"changes":     changes,
	})
	if err != nil {
		return fmt.Errorf("update ADR: %w", err)
	}

	// Post new diff as comment
	diffStr, _ := result["diff"].(string)
	if diffStr != "" {
		comment := fmt.Sprintf("## ADR Updated\n\n### Updated Changes\n\n```diff\n%s\n```\n\n**Accept:** /adr accept\n**Reject:** /adr reject [reason]",
			diffStr,
		)

		if err := h.postComment(ctx, repoFullName, prNumber, comment); err != nil {
			logger.Error("failed to post update comment", "err", err)
			return fmt.Errorf("post comment: %w", err)
		}
	}

	logger.Info("ADR update preview posted")
	return nil
}

// postHelpComment posts a help message explaining available commands.
func (h *CommandHandler) postHelpComment(ctx context.Context, repoFullName string, prNumber int) error {
	comment := `## ADR Pipeline Commands

Available commands:

- **/adr accept** - Accept the ADR proposal and create the record
- **/adr reject [reason]** - Reject the ADR proposal with optional reason
- **/adr update [changes]** - Update the ADR proposal and show new diff

Example usage:
` + "```" + `
/adr accept
/adr reject Not aligned with current architecture
/adr update Added section on performance implications
` + "```"

	return h.postComment(ctx, repoFullName, prNumber, comment)
}

// postComment posts a comment on a GitHub PR.
func (h *CommandHandler) postComment(ctx context.Context, repoFullName string, prNumber int, body string) error {
	if h.ghClient == nil {
		return fmt.Errorf("GitHub client not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	return h.ghClient.PostPRComment(ctx, repoFullName, prNumber, body)
}

// resolveProject determines the server project for a repository.
func (h *CommandHandler) resolveProject(repoFullName string) (ServerProject, error) {
	projectID := strings.ReplaceAll(repoFullName, "/", "-")

	return ServerProject{
		ProjectID: projectID,
		AdrRoot:   h.config.AdrRoot,
		RepoRoot:  h.config.RepoRoot,
	}, nil
}

// FindPendingPreview searches for an existing preview comment on the PR.
// This is used to update an existing preview rather than creating a new one.
func (h *CommandHandler) FindPendingPreview(ctx context.Context, repoFullName string, prNumber int) (*PRComment, error) {
	if h.ghClient == nil {
		return nil, fmt.Errorf("GitHub client not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	comments, err := h.ghClient.ListPRComments(ctx, repoFullName, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}

	// Look for the most recent preview comment
	for i := len(comments) - 1; i >= 0; i-- {
		if strings.Contains(comments[i].Body, h.config.Preview.CommentTag) {
			return &comments[i], nil
		}
	}

	return nil, nil
}
