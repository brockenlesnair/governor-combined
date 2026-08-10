package adr

import (
	"context"
	"fmt"
	"log/slog"
)

// AdrMcpClient is the interface for communicating with the ADR MCP server.
// In V1 this spawned .NET AdrMcp subprocesses. In combined, we use a Go stub
// until a native Go implementation is built.
type AdrMcpClient interface {
	// CallTool calls an ADR MCP tool by name with arguments.
	CallTool(ctx context.Context, tool string, args map[string]interface{}) (map[string]interface{}, error)
	// HealthCheck verifies the ADR server is responsive.
	HealthCheck(ctx context.Context) error
}

// GoStubAdrMcpClient is a stub implementation that returns placeholder results.
// Replace with real implementation when ADR logic is ported to Go.
type GoStubAdrMcpClient struct {
	logger *slog.Logger
}

// NewGoStubAdrMcpClient creates a new stub client.
func NewGoStubAdrMcpClient(logger *slog.Logger) *GoStubAdrMcpClient {
	return &GoStubAdrMcpClient{logger: logger}
}

// CallTool returns a placeholder result.
func (c *GoStubAdrMcpClient) CallTool(ctx context.Context, tool string, args map[string]interface{}) (map[string]interface{}, error) {
	c.logger.Warn("ADR MCP stub called", "tool", tool, "args", args)
	return map[string]interface{}{
		"status":  "stub",
		"message": fmt.Sprintf("ADR tool %s not yet implemented in Go", tool),
	}, nil
}

// HealthCheck always returns nil (stub is always healthy).
func (c *GoStubAdrMcpClient) HealthCheck(ctx context.Context) error {
	return nil
}
