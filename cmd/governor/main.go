package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brockenlesnair/governor-combined/config"
	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
	"github.com/brockenlesnair/governor-combined/pkg/docgov"
	"github.com/brockenlesnair/governor-combined/pkg/gateway"
	"github.com/brockenlesnair/governor-combined/pkg/integration"
	"github.com/brockenlesnair/governor-combined/pkg/staleness"
	"github.com/brockenlesnair/governor-combined/pkg/tools"
	"github.com/brockenlesnair/governor-combined/pkg/watcher"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const banner = `
  ╔══════════════════════════════════════════╗
  ║         Governor V2.0 MCP Server         ║
  ╚═══════════════════════════════════════════╝
`

var (
	// Metrics
	buildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "governor_build_info",
			Help: "Governor build information",
		},
		[]string{"version"},
	)
	graphNodes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "governor_graph_nodes_total",
			Help: "Total number of nodes in the call graph",
		},
	)
	graphEdges = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "governor_graph_edges_total",
			Help: "Total number of edges in the call graph",
		},
	)
	graphLastBuild = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "governor_graph_last_build_timestamp_seconds",
			Help: "Unix timestamp of last graph build",
		},
	)
	toolCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "governor_tool_calls_total",
			Help: "Total number of tool calls",
		},
		[]string{"tool", "status"},
	)
	toolLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "governor_tool_latency_seconds",
			Help:    "Tool call latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tool"},
	)
	watcherEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "governor_watcher_events_total",
			Help: "Total number of file watcher events",
		},
		[]string{"type"},
	)
	activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "governor_active_connections",
			Help: "Number of active WebSocket connections",
		},
	)
)

func init() {
	prometheus.MustRegister(buildInfo)
	prometheus.MustRegister(graphNodes)
	prometheus.MustRegister(graphEdges)
	prometheus.MustRegister(graphLastBuild)
	prometheus.MustRegister(toolCallsTotal)
	prometheus.MustRegister(toolLatency)
	prometheus.MustRegister(watcherEvents)
	prometheus.MustRegister(activeConnections)
}

func main() {
	configPath := flag.String("config", "governor.yaml", "path to governor config file")
	stdioMode := flag.Bool("stdio", false, "run MCP server over stdio")
	fullModeFlag := flag.Bool("full-mode", false, "enable full background maintenance mode")
	port := flag.Int("port", 0, "HTTP server port (overrides config)")
	projectRoot := flag.String("project-root", "", "project root directory (overrides config)")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override config with flags
	if *port != 0 {
		cfg.Features.Gateway.Listen = fmt.Sprintf(":%d", *port)
	}
	if *projectRoot != "" {
		cfg.Features.DocGov.ProjectRoot = *projectRoot
	}
	if *fullModeFlag {
		cfg.Features.Mode = "full"
	}
	fullMode := cfg.Features.FullModeEnabled()

	// Print banner and enabled features
	fmt.Fprint(os.Stderr, banner)
	printEnabledFeaturesTo(os.Stderr, cfg)

	// Create tools config
	toolsCfg := &tools.ToolsConfig{
		Port:            parsePort(cfg.Features.Gateway.Listen),
		LogLevel:        "info",
		ProjectRoot:     ".", // Default to current directory
		FeatureFlags:    map[string]bool{},
		GraphCachePath:  cfg.Features.Persist.DBPath,
		RebuildInterval: 5 * time.Minute,
		StdIOMode:       *stdioMode,
		Metrics: tools.MetricsConfig{
			Enabled: cfg.Features.Metrics.Enabled,
			Path:    cfg.Features.Metrics.Path,
		},
	}

	// Override project root from flag
	if *projectRoot != "" {
		toolsCfg.ProjectRoot = *projectRoot
	}

	// Initialize tools
	logger := slog.Default()
	gl, err := tools.NewGraphLifecycle(toolsCfg, logger)
	if err != nil {
		log.Fatalf("Failed to create graph lifecycle: %v", err)
	}
	defer gl.Close()

	th, err := tools.NewToolHandlers(gl, logger)
	if err != nil {
		log.Fatalf("Failed to create tool handlers: %v", err)
	}

	// Initialize extended governance components (rigour, sarif, adr, hangar, repo)
	govCfg := integration.DefaultConfig()
	governor, err := integration.New(logger, govCfg)
	if err != nil {
		log.Fatalf("Failed to create governor: %v", err)
	}
	defer governor.Stop()
	th.SetExtendedComponents(
		governor.Rigour,
		governor.SARIF,
		governor.ADRClient,
		governor.Hangar,
		governor.Repo,
	)

	// Create gateway
	gwCfg := &gateway.GatewayConfig{
		ToolDispatchUseRegistry: cfg.Features.Gateway.ToolDispatchUseRegistry,
	}
	gw := gateway.NewGateway(gwCfg, logger)

	// Register all 7 custom tools on the gateway
	if err := th.RegisterToolsOnGateway(gw); err != nil {
		log.Fatalf("Failed to register tools on gateway: %v", err)
	}

	// Initialize document governance
	var docRegistry *docgov.DocumentRegistry
	var docTools *docgov.DocGovTools

	ctx := context.Background()
	if fullMode {
		gl.StartRebuildLoop(ctx)
	}

	// Initialize document governance
	if cfg.Features.DocGov.Enabled {
		projectRoot := cfg.Features.DocGov.ProjectRoot
		if projectRoot == "" {
			projectRoot = toolsCfg.ProjectRoot
		}

		// Create staleness checker for docgov
		stalenessCfg := staleness.Config{
			ScanPaths:       []string{projectRoot},
			IncludePatterns: []string{"*.md", "*.adoc", "*.txt"},
			ExcludePatterns: []string{".git/*", "vendor/*", "node_modules/*"},
		}
		stalenessChk := staleness.NewChecker(stalenessCfg)

		// Create validator registry
		validatorReg := docgov.NewValidatorRegistry()

		// Create document registry
		docRegistry = docgov.NewDocumentRegistry(projectRoot, nil, validatorReg, stalenessChk)
		if err := docRegistry.Scan(ctx); err != nil {
			logger.Warn("Failed to scan documents", "error", err)
		}

		// Create docgov tools
		docTools = docgov.NewDocGovTools(docRegistry)
		if err := docTools.RegisterTools(gw.Registry()); err != nil {
			log.Fatalf("Failed to register docgov tools: %v", err)
		}

		// Start docgov watcher only in full mode.
		if fullMode {
			interval, _ := time.ParseDuration(cfg.Features.DocGov.WatcherInterval)
			if interval == 0 {
				interval = 30 * time.Second
			}
			watcherCfg := watcher.Config{
				Paths:           []string{projectRoot},
				PollInterval:    interval,
				IncludePatterns: []string{"*.md", "*.adoc", "*.txt"},
				ExcludePatterns: []string{".git/*", "vendor/*", "node_modules/*"},
			}
			docWatcher := watcher.NewWatcher(watcherCfg)
			docWatcher.Start(ctx)
			defer docWatcher.Stop()

			events := docWatcher.Subscribe()
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case evt := <-events:
						// Refresh document on change
						if docRegistry != nil {
							docRegistry.RefreshDocument(ctx, evt.Path)
						}
					}
				}
			}()
		}

		logger.Info("Document governance enabled", "project_root", projectRoot)
	}

	// Start extended governance components
	if err := governor.Start(ctx); err != nil {
		logger.Warn("Governor start failed (non-fatal)", "err", err)
	}

	// Start file watcher only in full mode.
	if fullMode {
		watcherCfg := watcher.Config{
			Paths:        []string{toolsCfg.ProjectRoot},
			PollInterval: 2 * time.Second,
		}
		w := watcher.NewWatcher(watcherCfg)
		w.Start(ctx)
		defer w.Stop()

		events := w.Subscribe()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case evt := <-events:
					watcherEvents.WithLabelValues(string(evt.EventType)).Inc()
					if evt.EventType == watcher.EventModified || evt.EventType == watcher.EventCreated || evt.EventType == watcher.EventDeleted {
						gl.RebuildGraphAsync(ctx)
					}
				}
			}
		}()
	}

	// Set build info
	buildInfo.WithLabelValues("v2.0.0").Set(1)

	// Start server
	if *stdioMode {
		runStdioServer(gw, gl, fullMode)
	} else {
		runHTTPServer(gw, cfg, gl)
	}
}

func runStdioServer(gw *gateway.Gateway, gl *tools.GraphLifecycle, fullMode bool) {
	fmt.Fprintln(os.Stderr, "Starting MCP server over stdio...")

	// Build graph in the background only in full mode.
	if fullMode {
		go func() {
			ctx := context.Background()
			g, err := gl.BuildGraph(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Initial graph build failed: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Graph built: %d nodes, %d edges\n", len(g.Nodes), len(g.Edges))
			}
		}()
	}

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	ctx := context.Background()

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
			sendStdioError(encoder, nil, -32700, "Parse error: "+err.Error())
			continue
		}

		var resp json.RawMessage
		var err error

		switch req.Method {
		case "initialize":
			resp, err = handleInitialize(ctx, req.Params)
		case "tools/list":
			resp, err = handleToolsList(ctx, gw, req.Params)
		case "tools/call":
			resp, err = handleToolCall(ctx, gw, req.Params)
		default:
			sendStdioError(encoder, req.ID, -32601, "Method not found: "+req.Method)
			continue
		}

		if err != nil {
			sendStdioError(encoder, req.ID, -32000, err.Error())
			continue
		}

		sendStdioResponse(encoder, req.ID, resp)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Stdin scanner error: %v", err)
	}
}

func handleInitialize(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "governor",
			"version": "2.0.0",
		},
	})
}

func handleToolsList(ctx context.Context, gw *gateway.Gateway, params json.RawMessage) (json.RawMessage, error) {
	tools := gw.ListTools()
	return json.Marshal(map[string]any{
		"tools": tools,
	})
}

func handleToolCall(ctx context.Context, gw *gateway.Gateway, params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid tool call params: %w", err)
	}

	if req.Arguments == nil {
		req.Arguments = make(map[string]any)
	}

	result, err := gw.ExecuteTool(ctx, req.Name, req.Arguments)
	if err != nil {
		return nil, err
	}

	return json.Marshal(map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(mustMarshal(result)),
			},
		},
	})
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func sendStdioResponse(encoder *json.Encoder, id json.RawMessage, result json.RawMessage) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  json.RawMessage(result),
	}
	encoder.Encode(resp)
}

func sendStdioError(encoder *json.Encoder, id json.RawMessage, code int, message string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	encoder.Encode(resp)
}

func runHTTPServer(gw *gateway.Gateway, cfg *config.GovernorConfig, gl *tools.GraphLifecycle) {
	addr := cfg.Features.Gateway.Listen
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","time":"%s"}`, time.Now().UTC().Format(time.RFC3339))
	})

	// Readiness check endpoint
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stats := gl.GraphStats()
		ready := stats.Nodes > 0
		status := 200
		if !ready {
			status = 503
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"ready":%v,"graph_nodes":%d}`, ready, stats.Nodes)
	})

	// List tools endpoint
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		tools := gw.ListTools()
		fmt.Fprintf(w, `{"tools":%v}`, tools)
	})

	// Metrics endpoint
	if cfg.Features.Metrics.Enabled {
		mux.Handle(cfg.Features.Metrics.Path, promhttp.Handler())
	}

	// WebSocket endpoint for MCP
	mux.HandleFunc("/mcp", gw.HandleWS())

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Update active connections metric
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			// This would need access to wsServer to get connected clients
			// activeConnections.Set(float64(wsServer.ConnectedClients()))
		}
	}()

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Fprintf(os.Stderr, "MCP server listening on %s\n", addr)
		fmt.Fprintln(os.Stderr, "  Endpoints: /health, /ready, /tools, /metrics, /mcp (WebSocket)")
		fmt.Fprintln(os.Stderr, "\nPress Ctrl+C to shut down.")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-done
	fmt.Fprintln(os.Stderr, "\nShutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}

	fmt.Fprintln(os.Stderr, "Server stopped.")
}

func updateGraphMetrics(g *callgraph.Graph) {
	graphNodes.Set(float64(len(g.Nodes)))
	graphEdges.Set(float64(len(g.Edges)))
	graphLastBuild.Set(float64(time.Now().Unix()))
}

func parsePort(listen string) int {
	// Simple port parsing from ":8080" format
	if len(listen) > 1 && listen[0] == ':' {
		var port int
		fmt.Sscanf(listen, ":%d", &port)
		return port
	}
	return 8080
}

func printEnabledFeaturesTo(w io.Writer, cfg *config.GovernorConfig) {
	fmt.Fprintln(w, "Enabled features:")
	fmt.Fprintf(w, "  - mode: %s\n", modeOrDefault(cfg.Features.Mode))

	features := []struct {
		name    string
		enabled bool
	}{
		{"search", cfg.Features.Search.Enabled},
		{"untested", cfg.Features.Untested.Enabled},
		{"deadcode", cfg.Features.Deadcode.Enabled},
		{"webhook", cfg.Features.Webhook.Enabled},
		{"httpproxy", cfg.Features.HTTPProxy.Enabled},
		{"memory", cfg.Features.Memory.Enabled},
		{"safety", cfg.Features.Safety.Enabled},
		{"callgraph", cfg.Features.Callgraph.Enabled},
		{"persist", cfg.Features.Persist.Enabled},
		{"gateway", cfg.Features.Gateway.Listen != ""},
		{"metrics", cfg.Features.Metrics.Enabled},
		{"docgov", cfg.Features.DocGov.Enabled},
		{"rigour", true},
		{"sarif", true},
		{"adr", true},
		{"hangar", false},
		{"repo-butler", false},
	}

	for _, f := range features {
		status := "✗"
		if f.enabled {
			status = "✓"
		}
		fmt.Fprintf(w, "  %s %s\n", status, f.name)
	}
	fmt.Fprintln(w)
}

func modeOrDefault(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "full") {
		return "full"
	}
	return "light"
}
