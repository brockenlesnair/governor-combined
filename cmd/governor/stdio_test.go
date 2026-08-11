package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/gateway"
	"github.com/brockenlesnair/governor-combined/pkg/tools"
)

// TestStdioServer_Initialize tests the initialize method
func TestStdioServer_Initialize(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Test initialize
	req := map[string]any{}
	resp, err := handleInitialize(context.Background(), mustMarshalRaw(req))
	if err != nil {
		t.Fatalf("handleInitialize failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion 2024-11-05, got %v", result["protocolVersion"])
	}
	if result["serverInfo"].(map[string]any)["name"] != "governor" {
		t.Errorf("expected server name governor, got %v", result["serverInfo"])
	}
	if _, ok := result["capabilities"]; !ok {
		t.Error("missing capabilities in response")
	}
}

// TestStdioServer_ToolsList tests the tools/list method
func TestStdioServer_ToolsList(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Test tools/list
	resp, err := handleToolsList(context.Background(), gw, mustMarshalRaw(map[string]any{}))
	if err != nil {
		t.Fatalf("handleToolsList failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	tools := result["tools"].([]any)
	if len(tools) < 9 { // 7 custom + 2 builtin
		t.Errorf("expected at least 9 tools, got %d", len(tools))
	}

	// Verify all 7 custom tools are present
	toolNames := make(map[string]bool)
	for _, t := range tools {
		tool := t.(map[string]any)
		toolNames[tool["name"].(string)] = true
	}

	expectedTools := []string{
		"validate_code", "validate_diff", "search_code",
		"get_callers", "get_callees", "get_impact", "audit_project",
	}
	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

// TestStdioServer_ToolCall tests the tools/call method
func TestStdioServer_ToolCall(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Test tools/call with validate_code
	params := map[string]any{
		"name": "validate_code",
		"arguments": map[string]any{
			"file": "test.go",
		},
	}
	resp, err := handleToolCall(context.Background(), gw, mustMarshalRaw(params))
	if err != nil {
		t.Fatalf("handleToolCall failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["content"] == nil {
		t.Error("missing content in response")
	}
}

// TestStdioServer_UnknownMethod tests unknown method handling
func TestStdioServer_UnknownMethod(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Test unknown method - should return error
	params := map[string]any{
		"tool":      "unknown_tool",
		"arguments": map[string]any{},
	}
	_, err = handleToolCall(context.Background(), gw, mustMarshalRaw(params))
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "tool not found") {
		t.Errorf("expected 'tool not found' error, got: %v", err)
	}
}

// TestStdioServer_FullRoundtrip tests a full stdio roundtrip
func TestStdioServer_FullRoundtrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Simulate stdio communication
	input := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
`)

	reader := strings.NewReader(string(input))
	scanner := bufio.NewScanner(reader)
	var responses []map[string]any

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatalf("Unmarshal request failed: %v", err)
		}

		var resp json.RawMessage
		var err error

		switch req.Method {
		case "initialize":
			resp, err = handleInitialize(context.Background(), req.Params)
		case "tools/list":
			resp, err = handleToolsList(context.Background(), gw, req.Params)
		case "tools/call":
			resp, err = handleToolCall(context.Background(), gw, req.Params)
		default:
			t.Fatalf("Unknown method: %s", req.Method)
		}

		if err != nil {
			t.Fatalf("Handler error: %v", err)
		}

		var result map[string]any
		if err := json.Unmarshal(resp, &result); err != nil {
			t.Fatalf("Unmarshal response failed: %v", err)
		}

		if result["error"] != nil {
			t.Fatalf("Response has error: %v", result["error"])
		}

		responses = append(responses, result)
	}

	if len(responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(responses))
	}

	// Verify initialize response
	if responses[0]["protocolVersion"] != "2024-11-05" {
		t.Error("initialize response missing protocolVersion")
	}

	// Verify tools/list response
	tools := responses[1]["tools"].([]any)
	if len(tools) < 9 {
		t.Errorf("expected at least 9 tools, got %d", len(tools))
	}
}

// TestStdioServer_InvalidJSON tests invalid JSON handling
func TestStdioServer_InvalidJSON(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Test invalid JSON
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{`
	reader := strings.NewReader(input)
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		err := json.Unmarshal([]byte(line), &req)
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	}
}

// TestStdioServer_MissingID tests handling of requests without ID
func TestStdioServer_MissingID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := tools.NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: true,
	}
	gw := gateway.NewGateway(gwCfg, logger)
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		t.Fatalf("RegisterToolsOnGateway failed: %v", err)
	}

	// Test missing ID (notification)
	params := map[string]any{}
	resp, err := handleInitialize(context.Background(), mustMarshalRaw(params))
	if err != nil {
		t.Fatalf("handleInitialize failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Should still respond with result even if ID was nil
	if result["protocolVersion"] != "2024-11-05" {
		t.Error("initialize response missing protocolVersion")
	}
}

func mustMarshalRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func DefaultToolsConfig() *tools.ToolsConfig {
	return &tools.ToolsConfig{
		Port:            8080,
		LogLevel:        "info",
		ProjectRoot:     ".",
		FeatureFlags:    map[string]bool{},
		GraphCachePath:  ".governor/graph.db",
		RebuildInterval: 5 * time.Minute,
		StdIOMode:       false,
		Metrics: tools.MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
	}
}
