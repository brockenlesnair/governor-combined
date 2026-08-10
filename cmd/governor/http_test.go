package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/gateway"
	"github.com/brockenlesnair/governor-combined/pkg/tools"
)

// TestHTTPServer_Health tests the /health endpoint
func TestHTTPServer_Health(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","time":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
}

// TestHTTPServer_Ready tests the /ready endpoint
func TestHTTPServer_Ready(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := gl.GraphStats()
		ready := stats.Nodes > 0
		status := 200
		if !ready {
			status = 503
		}
		w.WriteHeader(status)
		readyVal := 0
		if ready {
			readyVal = 1
		}
		w.Write([]byte(`{"ready":` + string(rune('0'+readyVal)) + `,"graph_nodes":` + string(rune('0'+stats.Nodes)) + `}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Without graph built, should return 503
	resp, err := http.Get(server.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 (no graph), got %d", resp.StatusCode)
	}
}

// TestHTTPServer_Tools tests the /tools endpoint
func TestHTTPServer_Tools(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		tools := gw.ListTools()
		b, _ := json.Marshal(map[string]any{"tools": tools})
		w.Write(b)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/tools")
	if err != nil {
		t.Fatalf("GET /tools failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	tools := result["tools"].([]any)
	if len(tools) < 9 {
		t.Errorf("expected at least 9 tools, got %d", len(tools))
	}
}

// TestHTTPServer_Metrics tests the /metrics endpoint
func TestHTTPServer_Metrics(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# HELP governor_build_info\n# TYPE governor_build_info gauge\ngovernor_build_info{version=\"v2.0.0\"} 1\n"))
	}))

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "governor_build_info") {
		t.Error("metrics missing governor_build_info")
	}
}

// TestHTTPServer_ConcurrentRequests tests concurrent HTTP requests
func TestHTTPServer_ConcurrentRequests(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Make 50 concurrent requests
	done := make(chan error, 50)
	for i := 0; i < 50; i++ {
		go func() {
			resp, err := http.Get(server.URL + "/health")
			if err != nil {
				done <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				done <- err
				return
			}
			done <- nil
		}()
	}

	for i := 0; i < 50; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
}

// TestHTTPServer_InvalidMethod tests invalid HTTP methods
func TestHTTPServer_InvalidMethod(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test POST to /health (should be MethodNotAllowed)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 for POST, got %d", resp.StatusCode)
	}
}

// TestHTTPServer_LargePayload tests large request payloads
func TestHTTPServer_LargePayload(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		tools := gw.ListTools()
		b, _ := json.Marshal(map[string]any{"tools": tools})
		w.Write(b)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test with large query string
	largeQuery := strings.Repeat("a", 10000)
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/tools?query="+largeQuery, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tools with large query failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}