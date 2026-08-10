// Package repo provides an MCP client for the Repo Butler governance server.
//
// It maps all 12 tools and 3 resources, handles the staleness envelope on every
// response (data_age_hours, commits_behind_main, warnings), and supports three
// staleness modes: strict (reject stale data), warn (log warning), and ignore.
package repo

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Staleness ────────────────────────────────────────────────────────

// StalenessMode controls how the client handles stale data.
type StalenessMode int

const (
	// StalenessStrict rejects responses where data_age_hours exceeds the threshold.
	StalenessStrict StalenessMode = iota
	// StalenessWarn logs a warning but returns the stale data.
	StalenessWarn
	// StalenessIgnore silently accepts stale data.
	StalenessIgnore
)

// StalenessEnvelope is present on every response from the Repo Butler server.
type StalenessEnvelope struct {
	DataAgeHours      float64  `json:"data_age_hours"`
	CommitsBehindMain int      `json:"commits_behind_main"`
	Warnings          []string `json:"warnings,omitempty"`
}

// IsStale returns true if the envelope exceeds the given threshold.
func (s StalenessEnvelope) IsStale(thresholdHours float64) bool {
	return s.DataAgeHours > thresholdHours
}

// ─── Configuration ────────────────────────────────────────────────────

// Config holds configuration for the Repo Butler MCP client.
type Config struct {
	// ServerCommand is the executable to launch (e.g. "node").
	ServerCommand string `yaml:"server_command"`

	// ServerArgs are the arguments for the server executable.
	ServerArgs []string `yaml:"server_args"`

	// ServerEnv are extra environment variables for the server process.
	ServerEnv []string `yaml:"server_env"`

	// FramingMode is "content-length" or "newline" (default: "newline" for node).
	FramingMode string `yaml:"framing_mode"`

	// StalenessMode controls how stale data is handled.
	StalenessMode StalenessMode `yaml:"staleness_mode"`

	// StalenessThresholdHours is the max age before data is considered stale.
	StalenessThresholdHours float64 `yaml:"staleness_threshold_hours"`

	// RequestTimeout is the timeout for individual MCP calls.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// InitTimeout is the timeout for the initialize handshake.
	InitTimeout time.Duration `yaml:"init_timeout"`
}

// applyDefaults fills in zero-valued fields with sensible defaults.
// It does not set ServerCommand; callers must supply that explicitly.
func (c *Config) applyDefaults() {
	if len(c.ServerArgs) == 0 {
		c.ServerArgs = []string{"src/mcp.js"}
	}
	if c.FramingMode == "" {
		c.FramingMode = "newline"
	}
	if c.StalenessThresholdHours == 0 {
		c.StalenessThresholdHours = 24.0
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 60 * time.Second
	}
	if c.InitTimeout == 0 {
		c.InitTimeout = 30 * time.Second
	}
}

// Validate checks the config for required fields without mutating state.
// Call applyDefaults before Validate if zero-value defaults are desired.
func (c *Config) Validate() error {
	if c.ServerCommand == "" {
		return fmt.Errorf("repo-butler: server_command is required")
	}
	if c.FramingMode != "" && c.FramingMode != "newline" && c.FramingMode != "content-length" {
		return fmt.Errorf("repo-butler: framing_mode must be \"newline\" or \"content-length\", got %q", c.FramingMode)
	}
	return nil
}

// ─── Tool Definitions ─────────────────────────────────────────────────

// ToolName identifies a Repo Butler MCP tool.
type ToolName string

const (
	ToolTriggerRefresh        ToolName = "trigger_refresh"
	ToolGetStaleness          ToolName = "get_staleness"
	ToolCheckConflicts        ToolName = "check_conflicts"
	ToolListDependencies      ToolName = "list_dependencies"
	ToolGetHealthTier         ToolName = "get_health_tier"
	ToolGetCampaignStatus     ToolName = "get_campaign_status"
	ToolQueryPortfolio        ToolName = "query_portfolio"
	ToolGetRepoMetadata       ToolName = "get_repo_metadata"
	ToolListPRs               ToolName = "list_prs"
	ToolGetCodeowners         ToolName = "get_codeowners"
	ToolCheckBranchProtection ToolName = "check_branch_protection"
	ToolAuditCompliance       ToolName = "audit_compliance"
	ToolCreateADR             ToolName = "create_adr"
)

// AllTools returns all 12 Repo Butler tool names.
func AllTools() []ToolName {
	return []ToolName{
		ToolTriggerRefresh,
		ToolGetStaleness,
		ToolCheckConflicts,
		ToolListDependencies,
		ToolGetHealthTier,
		ToolGetCampaignStatus,
		ToolQueryPortfolio,
		ToolGetRepoMetadata,
		ToolListPRs,
		ToolGetCodeowners,
		ToolCheckBranchProtection,
		ToolAuditCompliance,
	}
}

// ─── Resource Definitions ─────────────────────────────────────────────

// ResourceURI identifies a Repo Butler MCP resource.
type ResourceURI string

const (
	ResourceCurrentRepo   ResourceURI = "repo://current"
	ResourceChangeHistory ResourceURI = "repo://history"
	ResourcePortfolio     ResourceURI = "repo://portfolio"
)

// AllResources returns all 3 resource URIs.
func AllResources() []ResourceURI {
	return []ResourceURI{
		ResourceCurrentRepo,
		ResourceChangeHistory,
		ResourcePortfolio,
	}
}

// ─── MCP Transport (JSON-RPC 2.0 over stdio) ─────────────────────────

// mcpRequest is a JSON-RPC 2.0 request.
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpResponse is a JSON-RPC 2.0 response.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpNotification is a JSON-RPC 2.0 notification (no ID, no response expected).
type mcpNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// toolCallResult is the MCP tools/call result.
type toolCallResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// resourceReadResult is the MCP resources/read result.
type resourceReadResult struct {
	Contents []resourceContent `json:"contents"`
}

type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ─── Client ───────────────────────────────────────────────────────────

// Client is an MCP client for the Repo Butler governance server.
type Client struct {
	config      Config
	logger      *slog.Logger
	transport   *stdioTransport
	transportMu sync.Mutex
	seq         uint64
}

// stdioTransport manages the stdio connection to the MCP server process.
type stdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	pending map[string]chan rpcResult
	dead    chan struct{} // closed when the reader goroutine exits
	done    chan struct{} // closed after cmd.Wait returns
}

// rpcResult holds the outcome of a single JSON-RPC round trip.
type rpcResult struct {
	data json.RawMessage
	err  error
}

// NewClient creates a new Repo Butler MCP client.
// The client is not connected until Connect is called.
func NewClient(config Config, logger *slog.Logger) (*Client, error) {
	config.applyDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	c := &Client{
		config: config,
		logger: logger.With("component", "repo-butler-client"),
	}

	c.logger.Info("repo-butler client created",
		"server", config.ServerCommand,
		"args", config.ServerArgs,
		"staleness_mode", stalenessModeName(config.StalenessMode),
	)

	return c, nil
}

// Connect starts the MCP server process and establishes the stdio transport.
// It performs the MCP initialize handshake before returning.
func (c *Client) Connect(ctx context.Context) error {
	c.transportMu.Lock()
	defer c.transportMu.Unlock()

	if c.transport != nil && c.transport.cmd != nil && c.transport.cmd.Process != nil {
		return nil // already connected
	}

	initCtx, initCancel := context.WithTimeout(ctx, c.config.InitTimeout)
	defer initCancel()

	cmd := exec.CommandContext(ctx, c.config.ServerCommand, c.config.ServerArgs...)
	if len(c.config.ServerEnv) > 0 {
		cmd.Env = append(os.Environ(), c.config.ServerEnv...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	// Forward server stderr to the client logger for debugging.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				c.logger.Debug("server stderr", "output", strings.TrimSpace(string(buf[:n])))
			}
			if readErr != nil {
				return
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("start server process: %w", err)
	}

	t := &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		scanner: bufio.NewScanner(stdout),
		pending: make(map[string]chan rpcResult),
		dead:    make(chan struct{}),
		done:    make(chan struct{}),
	}

	c.transport = t

	// Start the reader goroutine that dispatches responses by request ID.
	go t.readLoop(c.logger)

	c.logger.Info("server process started", "pid", cmd.Process.Pid)

	// Perform the MCP initialize handshake.
	if err := c.doInitialize(initCtx); err != nil {
		_ = t.Close()
		return fmt.Errorf("mcp initialize: %w", err)
	}

	return nil
}

// doInitialize performs the MCP initialize → notifications/initialized handshake.
func (c *Client) doInitialize(ctx context.Context) error {
	initParams, err := mustMarshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "repo-butler-go-client",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("marshal init params: %w", err)
	}

	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"0"`),
		Method:  "initialize",
		Params:  initParams,
	}

	rawResult, err := c.callWithTimeout(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize request: %w", err)
	}

	// Validate the response is parseable JSON.
	var initResult json.RawMessage
	if err := json.Unmarshal(rawResult, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	// Send the initialized notification (no response expected from server).
	notif := mcpNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	notifBytes, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal initialized notification: %w", err)
	}
	if err := c.writeMessage(notifBytes); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	c.logger.Info("mcp initialize handshake complete")
	return nil
}

// Close gracefully shuts down the server process and releases resources.
func (c *Client) Close() error {
	c.transportMu.Lock()
	defer c.transportMu.Unlock()

	if c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// Close shuts down the transport: closes stdin, waits for process exit, force-kills on timeout.
func (t *stdioTransport) Close() error {
	// Close stdin to signal EOF to the server process.
	if t.stdin != nil {
		t.stdin.Close()
	}

	// Wait for the reader goroutine to finish.
	<-t.done

	// Wait for the process to exit gracefully.
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited gracefully.
	case <-time.After(5 * time.Second):
		// Force kill if the process didn't exit in time.
		_ = t.cmd.Process.Kill()
		<-done
	}

	return nil
}

// readLoop reads newline-delimited JSON-RPC messages from stdout and dispatches
// them to waiting callers via the pending map. It closes t.done when the loop exits
// and t.dead is closed implicitly via defer.
func (t *stdioTransport) readLoop(logger *slog.Logger) {
	defer close(t.done)

	for t.scanner.Scan() {
		line := t.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Try to parse as a JSON-RPC response.
		var resp mcpResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			logger.Warn("failed to parse server message", "err", err, "raw", string(line))
			continue
		}

		if resp.ID == nil {
			// This is a server notification, not a response.
			logger.Debug("server notification", "raw", string(line))
			continue
		}

		id := string(resp.ID)

		t.mu.Lock()
		ch, ok := t.pending[id]
		if ok {
			delete(t.pending, id)
		}
		t.mu.Unlock()

		if !ok {
			logger.Warn("response for unknown request id", "id", id)
			continue
		}

		if resp.Error != nil {
			ch <- rpcResult{
				err: fmt.Errorf("json-rpc error %d: %s", resp.Error.Code, resp.Error.Message),
			}
			continue
		}

		ch <- rpcResult{data: resp.Result}
	}

	if err := t.scanner.Err(); err != nil {
		logger.Error("stdout read error", "err", err)
	}

	// Close the dead channel to wake any callers blocked on pending requests.
	close(t.dead)
}

// writeMessage writes a newline-delimited JSON message to the server's stdin.
// It is safe for concurrent use.
func (c *Client) writeMessage(data []byte) error {
	c.transport.mu.Lock()
	defer c.transport.mu.Unlock()

	_, err := c.transport.stdin.Write(append(data, '\n'))
	return err
}

// ─── Tool Calls ───────────────────────────────────────────────────────

// CallTool invokes an MCP tool with the given arguments and returns the result
// with staleness envelope handling.
func (c *Client) CallTool(ctx context.Context, name ToolName, args map[string]any) (json.RawMessage, *StalenessEnvelope, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	c.logger.Debug("calling tool", "tool", string(name), "args", args)

	req, err := c.buildRequest("tools/call", map[string]any{"name": string(name), "arguments": args})
	if err != nil {
		return nil, nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	rawResult, err := c.callWithTimeout(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	// Parse the tool result.
	var toolResult toolCallResult
	if err := json.Unmarshal(rawResult, &toolResult); err != nil {
		return nil, nil, fmt.Errorf("parse tool result %s: %w", name, err)
	}

	if toolResult.IsError {
		errMsg := "tool error"
		if len(toolResult.Content) > 0 {
			errMsg = toolResult.Content[0].Text
		}
		return nil, nil, fmt.Errorf("tool %s failed: %s", name, errMsg)
	}

	// Extract and validate staleness envelope.
	envelope, data, err := extractStaleness(toolResult.Content)
	if err != nil {
		return nil, nil, fmt.Errorf("extract staleness for %s: %w", name, err)
	}

	if err := c.validateStaleness(envelope); err != nil {
		return nil, envelope, err
	}

	c.logger.Debug("tool call complete",
		"tool", string(name),
		"data_age_hours", envelope.DataAgeHours,
		"commits_behind", envelope.CommitsBehindMain,
		"warnings", len(envelope.Warnings),
	)

	return data, envelope, nil
}

// CallToolRaw invokes an MCP tool and returns the raw JSON without staleness extraction.
// Useful for tools that don't return the staleness envelope.
func (c *Client) CallToolRaw(ctx context.Context, name ToolName, args map[string]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	req, err := c.buildRequest("tools/call", map[string]any{"name": string(name), "arguments": args})
	if err != nil {
		return nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	rawResult, err := c.callWithTimeout(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	var toolResult toolCallResult
	if err := json.Unmarshal(rawResult, &toolResult); err != nil {
		return nil, fmt.Errorf("parse tool result %s: %w", name, err)
	}

	if toolResult.IsError {
		errMsg := "tool error"
		if len(toolResult.Content) > 0 {
			errMsg = toolResult.Content[0].Text
		}
		return nil, fmt.Errorf("tool %s failed: %s", name, errMsg)
	}

	if len(toolResult.Content) == 0 {
		return nil, nil
	}

	return json.RawMessage(toolResult.Content[0].Text), nil
}

// ─── Resource Reads ───────────────────────────────────────────────────

// ReadResource reads an MCP resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri ResourceURI) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	c.logger.Debug("reading resource", "uri", string(uri))

	req, err := c.buildRequest("resources/read", map[string]any{"uri": string(uri)})
	if err != nil {
		return nil, fmt.Errorf("read resource %s: %w", uri, err)
	}

	rawResult, err := c.callWithTimeout(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("read resource %s: %w", uri, err)
	}

	var readResult resourceReadResult
	if err := json.Unmarshal(rawResult, &readResult); err != nil {
		return nil, fmt.Errorf("parse resource result %s: %w", uri, err)
	}

	if len(readResult.Contents) == 0 {
		return nil, fmt.Errorf("resource %s returned no content", uri)
	}

	return json.RawMessage(readResult.Contents[0].Text), nil
}

// ─── Staleness Handling ───────────────────────────────────────────────

// extractStaleness extracts the staleness envelope from tool call content.
// The Repo Butler server wraps every response with metadata in the first content item.
func extractStaleness(content []textContent) (*StalenessEnvelope, json.RawMessage, error) {
	if len(content) == 0 {
		return &StalenessEnvelope{}, nil, fmt.Errorf("empty tool result content")
	}

	// The first content item contains the staleness envelope.
	var wrapper struct {
		Staleness *StalenessEnvelope `json:"staleness"`
		Data      json.RawMessage    `json:"data"`
	}

	if err := json.Unmarshal([]byte(content[0].Text), &wrapper); err != nil {
		// If parsing fails, treat as raw data with no staleness info.
		return &StalenessEnvelope{}, []byte(content[0].Text), nil
	}

	if wrapper.Staleness == nil {
		wrapper.Staleness = &StalenessEnvelope{}
	}

	return wrapper.Staleness, wrapper.Data, nil
}

// validateStaleness checks the staleness envelope against the configured mode and threshold.
func (c *Client) validateStaleness(envelope *StalenessEnvelope) error {
	if envelope == nil {
		return nil
	}

	if !envelope.IsStale(c.config.StalenessThresholdHours) {
		return nil
	}

	switch c.config.StalenessMode {
	case StalenessStrict:
		return fmt.Errorf("stale data rejected: data_age=%.1fh (threshold=%.1fh), commits_behind=%d",
			envelope.DataAgeHours, c.config.StalenessThresholdHours, envelope.CommitsBehindMain)
	case StalenessWarn:
		c.logger.Warn("stale data accepted with warning",
			"data_age_hours", envelope.DataAgeHours,
			"threshold_hours", c.config.StalenessThresholdHours,
			"commits_behind", envelope.CommitsBehindMain,
			"warnings", envelope.Warnings,
		)
		return nil
	case StalenessIgnore:
		return nil
	default:
		return nil
	}
}

// ─── MCP Transport Implementation ─────────────────────────────────────

// buildRequest constructs a JSON-RPC 2.0 request with a unique ID and marshaled params.
func (c *Client) buildRequest(method string, params any) (mcpRequest, error) {
	paramsBytes, err := mustMarshal(params)
	if err != nil {
		return mcpRequest{}, fmt.Errorf("marshal params for %s: %w", method, err)
	}
	return mcpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf(`"%d"`, atomic.AddUint64(&c.seq, 1))),
		Method:  method,
		Params:  paramsBytes,
	}, nil
}

// callWithTimeout sends a JSON-RPC request over stdio and waits for the matching response.
func (c *Client) callWithTimeout(ctx context.Context, req mcpRequest) (json.RawMessage, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.logger.Debug("sending request",
		"method", req.Method,
		"id", string(req.ID),
		"bytes", len(reqBytes),
	)

	c.transportMu.Lock()
	t := c.transport
	c.transportMu.Unlock()

	if t == nil {
		return nil, fmt.Errorf("not connected: call Connect first")
	}

	id := string(req.ID)
	ch := make(chan rpcResult, 1)

	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()

	// Ensure the pending entry is cleaned up if we return early (e.g. context cancelled).
	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	// Write the request to stdin.
	if err := c.writeMessage(reqBytes); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for the response, respecting context cancellation and process death.
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request %s timed out: %w", id, ctx.Err())
	case <-t.dead:
		return nil, fmt.Errorf("server process exited while waiting for response %s", id)
	case result := <-ch:
		if result.err != nil {
			return nil, result.err
		}
		return result.data, nil
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────

// mustMarshal marshals v to JSON. It propagates errors instead of silently swallowing them.
func mustMarshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return b, nil
}

// stalenessModeName returns a human-readable name for the staleness mode.
func stalenessModeName(m StalenessMode) string {
	switch m {
	case StalenessStrict:
		return "strict"
	case StalenessWarn:
		return "warn"
	case StalenessIgnore:
		return "ignore"
	default:
		return "unknown"
	}
}

// Fingerprint creates a SHA256 fingerprint for deduplication.
func Fingerprint(ruleID, filePath string, line int) string {
	h := sha256.New()
	h.Write([]byte(ruleID))
	h.Write([]byte{0})
	h.Write([]byte(filePath))
	h.Write([]byte{0})
	h.Write([]byte(fmt.Sprintf("%d", line)))
	return fmt.Sprintf("%x", h.Sum(nil))
}
