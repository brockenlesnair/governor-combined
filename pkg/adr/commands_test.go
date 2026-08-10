package adr

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseCommand_Accept(t *testing.T) {
	cmd := ParseSlashCommand("/adr accept 42")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Action != "accept" {
		t.Errorf("expected action 'accept', got %s", cmd.Action)
	}
	if len(cmd.Args) != 1 || cmd.Args[0] != "42" {
		t.Errorf("expected args ['42'], got %v", cmd.Args)
	}
	if cmd.Raw != "/adr accept 42" {
		t.Errorf("expected raw '/adr accept 42', got %q", cmd.Raw)
	}
}

func TestParseCommand_Reject(t *testing.T) {
	cmd := ParseSlashCommand("/adr reject 42 not aligned")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Action != "reject" {
		t.Errorf("expected action 'reject', got %s", cmd.Action)
	}
	if len(cmd.Args) != 3 {
		t.Errorf("expected 3 args, got %d", len(cmd.Args))
	}
	if cmd.Args[0] != "42" || cmd.Args[1] != "not" || cmd.Args[2] != "aligned" {
		t.Errorf("unexpected args: %v", cmd.Args)
	}
}

func TestParseCommand_Update(t *testing.T) {
	cmd := ParseSlashCommand("/adr update 42")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Action != "update" {
		t.Errorf("expected action 'update', got %s", cmd.Action)
	}
	if len(cmd.Args) != 1 || cmd.Args[0] != "42" {
		t.Errorf("expected args ['42'], got %v", cmd.Args)
	}
}

func TestParseCommand_Invalid(t *testing.T) {
	cmd := ParseSlashCommand("/adr deploy")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	// Unknown actions are treated as help
	if cmd.Action != "help" {
		t.Errorf("expected action 'help' for unknown command, got %s", cmd.Action)
	}
}

func TestParseCommand_NotSlashCommand(t *testing.T) {
	cmd := ParseSlashCommand("this is not a slash command")
	if cmd != nil {
		t.Error("expected nil for non-slash command")
	}
}

func TestParseCommand_BareAdr(t *testing.T) {
	cmd := ParseSlashCommand("/adr")
	if cmd == nil {
		t.Fatal("expected non-nil command for bare /adr")
	}
	if cmd.Action != "help" {
		t.Errorf("expected action 'help' for bare /adr, got %s", cmd.Action)
	}
}

func TestParseCommand_Help(t *testing.T) {
	cmd := ParseSlashCommand("/adr help")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Action != "help" {
		t.Errorf("expected action 'help', got %s", cmd.Action)
	}
}

func TestParseCommand_Whitespace(t *testing.T) {
	cmd := ParseSlashCommand("  /adr accept 42  ")
	if cmd == nil {
		t.Fatal("expected non-nil command with leading/trailing whitespace")
	}
	if cmd.Action != "accept" {
		t.Errorf("expected action 'accept', got %s", cmd.Action)
	}
}

func TestParseCommand_CaseInsensitive(t *testing.T) {
	cmd := ParseSlashCommand("/adr ACCEPT 42")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Action != "accept" {
		t.Errorf("expected action 'accept' (lowercase), got %s", cmd.Action)
	}
}

func TestNewCommandHandler(t *testing.T) {
	config := testConfig()
	client := NewGoStubAdrMcpClient(slog.Default())
	handler := NewCommandHandler(config, client, slog.Default())

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

func TestCommandHandler_SetGitHubClient(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ghClient := NewGitHubClient(testConfig(), "token", slog.Default())
	handler.SetGitHubClient(ghClient)

	if handler.ghClient != ghClient {
		t.Error("GitHub client not set correctly")
	}
}

func TestCommandHandler_HandleAccept(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ctx := context.Background()
	err := handler.HandleComment(ctx, "owner/repo", 42, "/adr accept", "user1")

	// Will fail on postComment because ghClient is nil, but that proves dispatch worked
	if err == nil {
		t.Error("expected error when ghClient is nil")
	}
	if !strings.Contains(err.Error(), "GitHub client not configured") {
		t.Errorf("expected 'GitHub client not configured' error, got: %v", err)
	}
}

func TestCommandHandler_HandleReject(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ctx := context.Background()
	err := handler.HandleComment(ctx, "owner/repo", 42, "/adr reject not aligned", "user1")

	// handleReject calls postComment which requires ghClient
	if err == nil {
		t.Error("expected error when ghClient is nil")
	}
	if !strings.Contains(err.Error(), "GitHub client not configured") {
		t.Errorf("expected 'GitHub client not configured' error, got: %v", err)
	}
}

func TestCommandHandler_HandleUpdate(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ctx := context.Background()
	// handleUpdate calls client.CallTool (succeeds with stub), then checks diffStr.
	// Stub returns no "diff" key, so diffStr is empty and postComment is skipped.
	// This means handleUpdate returns nil when ghClient is nil but no diff to post.
	err := handler.HandleComment(ctx, "owner/repo", 42, "/adr update add performance section", "user1")

	// The stub client returns a result without "diff", so the comment path is skipped.
	// HandleUpdate succeeds silently.
	if err != nil {
		t.Errorf("expected nil error for stub update (no diff to post), got: %v", err)
	}
}

func TestCommandHandler_HandleUnknown(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ctx := context.Background()
	// Unknown action triggers postHelpComment which requires ghClient
	err := handler.HandleComment(ctx, "owner/repo", 42, "/adr deploy", "user1")

	if err == nil {
		t.Error("expected error when ghClient is nil")
	}
}

func TestCommandHandler_HandleNonCommand(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ctx := context.Background()
	err := handler.HandleComment(ctx, "owner/repo", 42, "this is not a slash command", "user1")

	// Non-slash commands should return nil (ignored)
	if err != nil {
		t.Errorf("expected nil for non-command, got: %v", err)
	}
}

func TestCommandHandler_HandleReject_NoReason(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	ctx := context.Background()
	// Reject with no reason args
	err := handler.HandleComment(ctx, "owner/repo", 42, "/adr reject", "user1")

	// Should still fail on postComment, but with "No reason provided" default
	if err == nil {
		t.Error("expected error when ghClient is nil")
	}
}

func TestCommandHandler_ResolveProject(t *testing.T) {
	handler := NewCommandHandler(testConfig(), NewGoStubAdrMcpClient(slog.Default()), slog.Default())

	project, err := handler.resolveProject("myorg/myrepo")
	if err != nil {
		t.Fatal(err)
	}

	if project.ProjectID != "myorg-myrepo" {
		t.Errorf("expected project ID 'myorg-myrepo', got %s", project.ProjectID)
	}
}
