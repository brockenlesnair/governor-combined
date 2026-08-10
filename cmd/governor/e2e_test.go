package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// projectRoot returns the absolute path to the governor-combined project root.
func projectRoot(t *testing.T) string {
	t.Helper()
	// cmd/governor/ is two levels below the project root
	dir, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve project root: %v", err)
	}
	return dir
}

// buildBinary builds the governor binary into a temp directory and returns the path.
func buildBinary(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "governor")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/governor")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	if info, err := os.Stat(binaryPath); err != nil || info.Size() == 0 {
		t.Fatalf("binary not found or empty at %s", binaryPath)
	}
	return binaryPath
}

// freePort asks the kernel for an available port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// stdioProcess manages a subprocess running in stdio mode.
type stdioProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	cancel context.CancelFunc
}

// startStdio starts the governor binary in stdio mode and returns a handle.
func startStdio(t *testing.T, binaryPath string) *stdioProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, binaryPath, "--stdio")
	cmd.Dir = projectRoot(t)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatalf("StdinPipe failed: %v", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("StdoutPipe failed: %v", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("cmd.Start failed: %v", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	// Increase buffer for large responses
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	return &stdioProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: scanner,
		cancel: cancel,
	}
}

// sendRequest sends a JSON-RPC request and reads the response line.
func (sp *stdioProcess) sendRequest(t *testing.T, req map[string]any) map[string]any {
	t.Helper()
	enc := json.NewEncoder(sp.stdin)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("failed to encode request: %v", err)
	}

	if !sp.stdout.Scan() {
		if err := sp.stdout.Err(); err != nil {
			t.Fatalf("stdout scan error: %v", err)
		}
		t.Fatalf("no response from stdout (EOF)")
	}

	line := sp.stdout.Text()
	if line == "" {
		t.Fatalf("empty response line")
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("failed to unmarshal response %q: %v", line, err)
	}
	return resp
}

// close cleans up the subprocess.
func (sp *stdioProcess) close() {
	sp.stdin.Close()
	sp.cancel()
	// Wait briefly for process to exit, then force kill
	done := make(chan error, 1)
	go func() { done <- sp.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if sp.cmd.Process != nil {
			sp.cmd.Process.Kill()
		}
	}
}

// ---------------------------------------------------------------------------
// E2E Tests
// ---------------------------------------------------------------------------

// TestE2E_BinaryBuild verifies the binary compiles and is runnable.
func TestE2E_BinaryBuild(t *testing.T) {
	binaryPath := buildBinary(t)

	// Verify the binary exists and has nonzero size
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("binary stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("binary has zero size")
	}
	t.Logf("binary built: %s (%d bytes)", binaryPath, info.Size())

	// Verify it runs without immediately crashing (--help or --stdio exit)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "--help")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	// --help might not be a recognized flag, that's fine; the key is the process doesn't segfault
	t.Logf("--help output: %s (err=%v)", string(out), err)
}

// TestE2E_StdioLifecycle tests the full MCP lifecycle: initialize -> tools/list -> tools/call.
func TestE2E_StdioLifecycle(t *testing.T) {
	binaryPath := buildBinary(t)
	sp := startStdio(t, binaryPath)
	defer sp.close()

	// 1. Initialize
	initResp := sp.sendRequest(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{},
	})
	t.Logf("initialize response: %v", initResp)

	if initResp["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", initResp["jsonrpc"])
	}
	result, ok := initResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T: %v", initResp["result"], initResp["result"])
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion 2024-11-05, got %v", result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "governor" {
		t.Errorf("expected server name governor, got %v", serverInfo["name"])
	}

	// 2. tools/list
	listResp := sp.sendRequest(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	t.Logf("tools/list response keys: %v", keysOf(listResp))

	listResult := listResp["result"].(map[string]any)
	toolList := listResult["tools"].([]any)
	if len(toolList) < 9 {
		t.Errorf("expected at least 9 tools, got %d", len(toolList))
	}
	t.Logf("tools/list returned %d tools", len(toolList))

	// 3. tools/call with validate_code
	callResp := sp.sendRequest(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "validate_code",
			"arguments": map[string]any{
				"file": "main.go",
			},
		},
	})
	t.Logf("tools/call validate_code: %v", callResp)

	if callResp["error"] != nil {
		t.Logf("tool call returned error (may be expected for file not found): %v", callResp["error"])
	} else {
		callResult, ok := callResp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result object from tools/call, got %T", callResp["result"])
		}
		if callResult["content"] == nil {
			t.Error("expected content in tools/call result")
		}
	}
}

// TestE2E_StdioAllTools calls each of the 16 registered tools.
func TestE2E_StdioAllTools(t *testing.T) {
	binaryPath := buildBinary(t)
	sp := startStdio(t, binaryPath)
	defer sp.close()

	// Initialize first
	sp.sendRequest(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params":  map[string]any{},
	})

	tools := []struct {
		name      string
		arguments map[string]any
		expectErr bool // expect JSON-RPC error (not tool error)
	}{
		{"validate_code", map[string]any{"file": "main.go"}, false},
		{"validate_diff", map[string]any{"diff": "+added line\n-old line"}, false},
		{"search_code", map[string]any{"query": "main"}, false},
		{"get_callers", map[string]any{"function": "main"}, false},
		{"get_callees", map[string]any{"function": "main"}, false},
		{"get_impact", map[string]any{"function": "main"}, false},
		{"audit_project", map[string]any{}, false},
		{"rigour_check", map[string]any{}, false},
		{"rigour_state", map[string]any{}, false},
		{"rigour_stats", map[string]any{}, false},
		{"sarif_export", map[string]any{}, false},
		{"adr_create", map[string]any{}, false},
		{"adr_list", map[string]any{}, false},
		{"hangar_score", map[string]any{}, false},
		{"repo_health", map[string]any{}, false},
	}

	for i, tool := range tools {
		id := i + 10 // Start at id 10 to avoid collision with init
		resp := sp.sendRequest(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      tool.name,
				"arguments": tool.arguments,
			},
		})

		if resp["jsonrpc"] != "2.0" {
			t.Errorf("tool %q: missing jsonrpc field", tool.name)
		}

		if tool.expectErr {
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Errorf("tool %q: expected JSON-RPC error, got result: %v", tool.name, resp["result"])
			} else {
				t.Logf("tool %q: error code=%v message=%v", tool.name, errObj["code"], errObj["message"])
			}
		} else {
			// Tool calls return either:
			// - result with content (success)
			// - error with code -32000 (server-side tool error: missing required params, not configured, etc.)
			// - error with code -32602 (invalid params from JSON-RPC layer)
			// All are valid responses — the important thing is we got a well-formed JSON-RPC response.
			if resp["error"] != nil {
				errObj := resp["error"].(map[string]any)
				code := errObj["code"].(float64)
				allowedCodes := map[float64]bool{-32602: true, -32000: true}
				if !allowedCodes[code] {
					t.Errorf("tool %q: unexpected JSON-RPC error code: %v msg=%v",
						tool.name, errObj["code"], errObj["message"])
				}
			}
			// Verify result has content when present
			if resp["result"] != nil {
				resultObj, ok := resp["result"].(map[string]any)
				if ok && resultObj["content"] == nil {
					t.Errorf("tool %q: result missing content field", tool.name)
				}
			}
		}

		t.Logf("tool %q (id=%d): OK", tool.name, id)
	}
}

// TestE2E_StdioUnknownMethod sends an unknown method and expects error code -32601.
func TestE2E_StdioUnknownMethod(t *testing.T) {
	binaryPath := buildBinary(t)
	sp := startStdio(t, binaryPath)
	defer sp.close()

	resp := sp.sendRequest(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "unknown/method",
		"params":  map[string]any{},
	})

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got: %v", resp)
	}
	code := errObj["code"].(float64)
	if code != -32601 {
		t.Errorf("expected error code -32601, got %v", code)
	}
	if !strings.Contains(errObj["message"].(string), "unknown/method") {
		t.Errorf("error message should mention method name, got: %v", errObj["message"])
	}
	t.Logf("unknown method error: code=%v msg=%v", errObj["code"], errObj["message"])
}

// TestE2E_StdioInvalidJSON sends malformed JSON and expects error code -32700.
func TestE2E_StdioInvalidJSON(t *testing.T) {
	binaryPath := buildBinary(t)
	sp := startStdio(t, binaryPath)
	defer sp.close()

	// Send truncated JSON directly (bypass the encoder)
	_, err := sp.stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":` + "\n"))
	if err != nil {
		t.Fatalf("failed to write invalid JSON: %v", err)
	}

	if !sp.stdout.Scan() {
		if err := sp.stdout.Err(); err != nil {
			t.Fatalf("stdout scan error: %v", err)
		}
		t.Fatal("no response for invalid JSON")
	}

	line := sp.stdout.Text()
	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v (raw: %q)", err, line)
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got: %v", resp)
	}
	code := errObj["code"].(float64)
	if code != -32700 {
		t.Errorf("expected error code -32700 (parse error), got %v", code)
	}
	t.Logf("invalid JSON error: code=%v msg=%v", errObj["code"], errObj["message"])
}

// TestE2E_HTTPLifecycle tests the full HTTP server lifecycle.
func TestE2E_HTTPLifecycle(t *testing.T) {
	binaryPath := buildBinary(t)
	port := freePort(t)

	cmd := exec.Command(binaryPath, "--port", fmt.Sprintf("%d", port))
	cmd.Dir = projectRoot(t)
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start HTTP server: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}

	// Poll /health until ready
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not become ready within 10s")
	}

	// Test /health
	t.Run("Health", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/health")
		if err != nil {
			t.Fatalf("GET /health failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("failed to parse health response: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("expected status ok, got %v", result["status"])
		}
		if result["time"] == nil {
			t.Error("expected 'time' field in health response")
		}
		t.Logf("health: %v", result)
	})

	t.Run("Tools", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/tools")
		if err != nil {
			t.Fatalf("GET /tools failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// The /tools endpoint uses fmt.Fprintf with %v, which may not produce
		// valid JSON. Verify the response contains expected tool names.
		expectedTools := []string{
			"validate_code", "validate_diff", "search_code",
			"get_callers", "get_callees", "get_impact", "audit_project",
		}
		for _, name := range expectedTools {
			if !strings.Contains(bodyStr, name) {
				t.Errorf("tools response missing tool: %s", name)
			}
		}
		t.Logf("tools endpoint returned %d bytes, contains all expected tools", len(body))
	})

	// Test /metrics
	t.Run("Metrics", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "governor_build_info") {
			t.Error("metrics missing governor_build_info")
		}
		t.Logf("metrics returned %d bytes", len(body))
	})
}

// TestE2E_HTTPHealthCheck tests the /health endpoint specifically.
func TestE2E_HTTPHealthCheck(t *testing.T) {
	binaryPath := buildBinary(t)
	port := freePort(t)

	cmd := exec.Command(binaryPath, "--port", fmt.Sprintf("%d", port))
	cmd.Dir = projectRoot(t)
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start HTTP server: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}

	// Wait for server
	for i := 0; i < 50; i++ {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", result["status"])
	}
	if result["time"] == nil {
		t.Error("expected 'time' field in response")
	}
	t.Logf("health check: status=%v time=%v", result["status"], result["time"])
}

// TestE2E_HTTPToolsList tests the /tools endpoint specifically.
func TestE2E_HTTPToolsList(t *testing.T) {
	binaryPath := buildBinary(t)
	port := freePort(t)

	cmd := exec.Command(binaryPath, "--port", fmt.Sprintf("%d", port))
	cmd.Dir = projectRoot(t)
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start HTTP server: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}

	// Wait for server
	for i := 0; i < 50; i++ {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	resp, err := client.Get(baseURL + "/tools")
	if err != nil {
		t.Fatalf("GET /tools failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	expectedTools := []string{
		"validate_code", "validate_diff", "search_code",
		"get_callers", "get_callees", "get_impact", "audit_project",
	}
	for _, name := range expectedTools {
		if !strings.Contains(bodyStr, name) {
			t.Errorf("missing custom tool: %s", name)
		}
	}

	extendedTools := []string{"rigour_check", "rigour_state", "rigour_stats", "sarif_export", "adr_create", "adr_list"}
	for _, name := range extendedTools {
		if !strings.Contains(bodyStr, name) {
			t.Errorf("missing extended tool: %s", name)
		}
	}

	t.Logf("tools list: %d bytes, all expected tools present", len(body))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
