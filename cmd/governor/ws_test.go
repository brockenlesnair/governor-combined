package main

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/gateway"
	"github.com/brockenlesnair/governor-combined/pkg/tools"
	"github.com/gorilla/websocket"
)

// TestWebSocket_Connect tests basic WebSocket connection
func TestWebSocket_Connect(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send tools/list
	req := map[string]any{
		"id":     "1",
		"method": "tools/list",
		"params": map[string]any{},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	// Read response
	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if resp["id"] != "1" {
		t.Errorf("expected id 1, got %v", resp["id"])
	}
	if resp["result"] == nil {
		t.Error("missing result in response")
	}
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 9 {
		t.Errorf("expected at least 9 tools, got %d", len(tools))
	}
}

// TestWebSocket_ToolCall tests tool calling over WebSocket
func TestWebSocket_ToolCall(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send tools/call
	req := map[string]any{
		"id":     "2",
		"method": "tools/call",
		"params": map[string]any{
			"tool":      "validate_code",
			"arguments": map[string]any{"file": "test.go"},
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	// Read response
	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if resp["id"] != "2" {
		t.Errorf("expected id 2, got %v", resp["id"])
	}
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
	if resp["result"] == nil {
		t.Error("missing result in response")
	}
}

// TestWebSocket_InvalidMethod tests invalid method over WebSocket
func TestWebSocket_InvalidMethod(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send invalid method
	req := map[string]any{
		"id":     "3",
		"method": "invalid/method",
		"params": map[string]any{},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if resp["error"] == nil {
		t.Error("expected error for invalid method")
	}
	errCode, ok := resp["error"].(map[string]any)["code"].(float64)
	if !ok || int(errCode) != -32601 {
		t.Errorf("expected error code -32601, got %v", resp["error"])
	}
}

// TestWebSocket_UnknownTool tests unknown tool over WebSocket
func TestWebSocket_UnknownTool(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send tools/call with unknown tool
	req := map[string]any{
		"id":     "4",
		"method": "tools/call",
		"params": map[string]any{
			"tool":      "unknown_tool",
			"arguments": map[string]any{},
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if resp["error"] == nil {
		t.Error("expected error for unknown tool")
	}
	errCode, ok := resp["error"].(map[string]any)["code"].(float64)
	if !ok || int(errCode) != -32601 {
		t.Errorf("expected error code -32601, got %v", resp["error"])
	}
}

// TestWebSocket_MultipleRequests tests multiple sequential requests
func TestWebSocket_MultipleRequests(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send multiple requests
	for i := 0; i < 10; i++ {
		req := map[string]any{
			"id":     string(rune('0' + i)),
			"method": "tools/list",
			"params": map[string]any{},
		}
		if err := conn.WriteJSON(req); err != nil {
			t.Fatalf("WriteJSON failed: %v", err)
		}

		var resp map[string]any
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("ReadJSON failed: %v", err)
		}

		if resp["id"] != string(rune('0'+i)) {
			t.Errorf("expected id %d, got %v", i, resp["id"])
		}
	}
}

// TestWebSocket_ConcurrentConnections tests multiple concurrent connections
func TestWebSocket_ConcurrentConnections(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"

	// Create 10 concurrent connections
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				done <- err
				return
			}
			defer conn.Close()

			req := map[string]any{
				"id":     "1",
				"method": "tools/list",
				"params": map[string]any{},
			}
			if err := conn.WriteJSON(req); err != nil {
				done <- err
				return
			}

			var resp map[string]any
			if err := conn.ReadJSON(&resp); err != nil {
				done <- err
				return
			}

			if resp["result"] == nil {
				done <- err
				return
			}

			done <- nil
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent connection failed: %v", err)
		}
	}
}

// TestWebSocket_PingPong tests ping/pong keepalive
func TestWebSocket_PingPong(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send ping
	if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("WriteControl failed: %v", err)
	}

	// Read pong (handled automatically by gorilla/websocket)
	// Just verify connection is still alive by sending a request
	req := map[string]any{
		"id":     "1",
		"method": "tools/list",
		"params": map[string]any{},
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if resp["result"] == nil {
		t.Error("missing result after ping")
	}
}

// TestWebSocket_Close tests graceful connection close
func TestWebSocket_Close(t *testing.T) {
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

	server := httptest.NewServer(gw.HandleWS())
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/mcp"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}

	// Close connection
	if err := conn.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify we can create a new connection
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Second WebSocket dial failed: %v", err)
	}
	defer conn2.Close()

	req := map[string]any{
		"id":     "1",
		"method": "tools/list",
		"params": map[string]any{},
	}
	if err := conn2.WriteJSON(req); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var resp map[string]any
	if err := conn2.ReadJSON(&resp); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if resp["result"] == nil {
		t.Error("missing result in second connection")
	}
}
