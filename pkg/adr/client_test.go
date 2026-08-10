package adr

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
)

func TestNewGoStubAdrMcpClient(t *testing.T) {
	client := NewGoStubAdrMcpClient(slog.Default())

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestGoStubAdrMcpClient_CallTool(t *testing.T) {
	client := NewGoStubAdrMcpClient(slog.Default())
	ctx := context.Background()

	result, err := client.CallTool(ctx, "create_adr", map[string]interface{}{
		"title":  "Test ADR",
		"status": "proposed",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result["status"] != "stub" {
		t.Errorf("expected status 'stub', got %v", result["status"])
	}

	expectedMsg := "ADR tool create_adr not yet implemented in Go"
	if result["message"] != expectedMsg {
		t.Errorf("unexpected message: %v", result["message"])
	}
}

func TestGoStubAdrMcpClient_CallTool_AllTools(t *testing.T) {
	client := NewGoStubAdrMcpClient(slog.Default())
	ctx := context.Background()

	tools := []string{"create_adr", "update_adr", "delete_adr", "list_adrs", "search_adrs", "get_adr"}

	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			result, err := client.CallTool(ctx, tool, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result["status"] != "stub" {
				t.Errorf("expected status 'stub' for tool %s", tool)
			}
			expectedMsg := fmt.Sprintf("ADR tool %s not yet implemented in Go", tool)
			if result["message"] != expectedMsg {
				t.Errorf("unexpected message for tool %s: %v", tool, result["message"])
			}
		})
	}
}

func TestGoStubAdrMcpClient_CallTool_NilArgs(t *testing.T) {
	client := NewGoStubAdrMcpClient(slog.Default())
	ctx := context.Background()

	result, err := client.CallTool(ctx, "list_adrs", nil)
	if err != nil {
		t.Fatal(err)
	}

	if result["status"] != "stub" {
		t.Errorf("expected status 'stub', got %v", result["status"])
	}
}

func TestGoStubAdrMcpClient_HealthCheck(t *testing.T) {
	client := NewGoStubAdrMcpClient(slog.Default())
	ctx := context.Background()

	err := client.HealthCheck(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGoStubAdrMcpClient_ImplementsInterface(t *testing.T) {
	// Verify GoStubAdrMcpClient implements AdrMcpClient
	var _ AdrMcpClient = (*GoStubAdrMcpClient)(nil)
}

func TestGoStubAdrMcpClient_CallTool_WithComplexArgs(t *testing.T) {
	client := NewGoStubAdrMcpClient(slog.Default())
	ctx := context.Background()

	args := map[string]interface{}{
		"title":       "Use event sourcing",
		"status":      "proposed",
		"previewOnly": true,
		"diff":        "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,4 @@\n+import \"event\"",
		"pr_number":   42,
		"repo":        "owner/repo",
		"metadata": map[string]string{
			"source": "ci-pipeline",
		},
	}

	result, err := client.CallTool(ctx, "create_adr", args)
	if err != nil {
		t.Fatal(err)
	}

	if result["status"] != "stub" {
		t.Errorf("expected status 'stub', got %v", result["status"])
	}
}
