# Governor

**Multi-language code quality enforcement platform — V1 + V2 merged**

Governor is an MCP (Model Context Protocol) server that enforces code quality, security, and documentation standards across your codebase. It combines static analysis, call graph intelligence, document governance, and architectural decision tracking into a single toolset your AI agents can call.

## Features

| Feature | Description |
|---------|-------------|
| **Code Validation** | Detect dangerous patterns, policy violations, and security issues |
| **Diff Validation** | Validate only changed lines in a diff for safety issues |
| **Call Graph Analysis** | Track callers, callees, and impact across your codebase |
| **Search** | Fuzzy, regex, or type-aware code symbol search |
| **Dead Code Detection** | Identify unused code with configurable confidence thresholds |
| **Untested Code Detection** | Find untested exported functions |
| **Project Auditing** | Run comprehensive audits covering safety, coverage, and dead code |
| **DLP Pre-filter** | Data Loss Prevention scan before agent processing |
| **Rigour Supervisor** | Adaptive code quality coordinator with brain-based pattern learning |
| **SARIF Export** | Export findings as SARIF 2.1.0 reports for toolchain integration |
| **ADR Management** | Create and list Architecture Decision Records |
| **Document Governance** | Validate documentation against templates and schemas |
| **Hangar Scorecard** | Fleet-wide compliance scorecard |
| **Repo Butler** | Repository health tier assessment |
| **File Watching** | Auto-rebuild call graph on file changes |
| **Prometheus Metrics** | Built-in observability with `/metrics` endpoint |

## MCP Tools (15)

Governor exposes 15 tools via the MCP protocol:

### V2 Core Tools (7)

| Tool | Description |
|------|-------------|
| `validate_code` | Validate a source file for dangerous patterns, policy violations, and security issues |
| `validate_diff` | Validate only the changed lines in a diff for safety issues |
| `search_code` | Search for code symbols using fuzzy, regex, or type-aware queries |
| `get_callers` | Get all callers of a function (direct or transitive) |
| `get_callees` | Get all callees of a function (direct or transitive) |
| `get_impact` | Analyze the impact of changing a function |
| `audit_project` | Run a full project audit: untested code, dead code, and safety issues |

### V1 Extended Tools (8)

| Tool | Description |
|------|-------------|
| `rigour_check` | Run DLP pre-filter scan on code text before agent processing |
| `rigour_state` | Get current rigour supervisor state (idle, working, fixing, etc.) |
| `rigour_stats` | Get rigour brain statistics: pattern count, hard rules, avg strength |
| `sarif_export` | Export findings as a SARIF 2.1.0 report |
| `adr_create` | Create an Architecture Decision Record from a template |
| `adr_list` | List existing Architecture Decision Records |
| `hangar_score` | Get the fleet compliance scorecard from Hangar |
| `repo_health` | Get the repo health tier from Repo Butler |

## Quick Start

### Prerequisites

- Go 1.25+
- (Optional) [ast-grep](https://github.com/ast-grep/ast-grep) CLI for advanced rigour checks

### Build

```bash
git clone https://github.com/brockenlesnair/governor-combined.git
cd governor-combined
go build -o governor ./cmd/governor
```

### Run

**HTTP mode** (default):

```bash
./governor --config governor.yaml
```

Server starts on `:8080` with these endpoints:

| Endpoint | Purpose |
|----------|---------|
| `/health` | Health check |
| `/ready` | Readiness check (graph must be built) |
| `/tools` | List available MCP tools |
| `/mcp` | WebSocket MCP connection |
| `/metrics` | Prometheus metrics |

**Stdio mode** (for MCP integrations):

```bash
./governor --stdio
```

Reads JSON-RPC from stdin, writes responses to stdout.

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `governor.yaml` | Path to config file |
| `--stdio` | `false` | Run MCP server over stdio |
| `--port` | `0` | HTTP server port (overrides config) |
| `--project-root` | `""` | Project root directory (overrides config) |

## Configuration

Governor is configured via `governor.yaml`:

```yaml
features:
  search:
    enabled: true
    max_results: 50
    fuzzy_threshold: 0.3
  untested:
    enabled: true
    include_exported: true
    min_priority: 0.3
  deadcode:
    enabled: true
    exclude_exported: true
    min_confidence: 0.5
  safety:
    enabled: true
    audit_path: "./data/audit.log"
  callgraph:
    enabled: true
  persist:
    enabled: true
    db_path: "./data/governor.db"
  gateway:
    tool_dispatch_use_registry: true
    listen: ":8080"
  metrics:
    enabled: true
    path: "/metrics"
  docgov:
    enabled: true
    project_root: "."
    enable_watcher: true
    watcher_interval: "30s"
  rigour:
    enabled: true
    dlp_enabled: true
    entropy_threshold: 5.5
  sarif:
    enabled: true
    output_path: "./data/sarif"
    dedup_window: 24h
  adr:
    enabled: true
    adr_root: "./docs/adr"
    repo_root: "."
  hangar:
    enabled: false          # Set true when connected to a Hangar fleet
    endpoint: ""
    token: ""
  repo:
    enabled: false          # Set true when Repo Butler is configured
    butler_path: ""
```

## Architecture

### Packages (22)

| Package | Purpose |
|---------|---------|
| `cmd/governor` | Entry point, CLI flags, HTTP/stdio servers |
| `config` | YAML config loading and validation |
| `pkg/adr` | Architecture Decision Records (CI hook, commands, GitHub integration) |
| `pkg/callgraph` | Call graph builder, analyzer, and tree-sitter integration |
| `pkg/collab` | Collaboration features |
| `pkg/deadcode` | Dead code detection engine |
| `pkg/docgov` | Document governance — templates, validators, schema enforcement |
| `pkg/gateway` | MCP gateway — tool registration, dispatch, WebSocket handling |
| `pkg/hangar` | Hangar fleet compliance client |
| `pkg/httpproxy` | HTTP proxy middleware |
| `pkg/integration` | Governor wiring — connects all components into the `Governor` struct |
| `pkg/mcp` | MCP protocol types and helpers |
| `pkg/memory` | Persistent memory storage |
| `pkg/persist` | SQLite persistence layer |
| `pkg/repo` | Repo Butler client — health tiers, council, portfolio, triggers |
| `pkg/rigour` | Rigour Supervisor — brain, DLP, state machine, fix packets, strategy |
| `pkg/safety` | Safety validation — builtin policies, pattern detection |
| `pkg/sarif` | SARIF 2.1.0 export with deduplication and mapper pipeline |
| `pkg/search` | Code search engine |
| `pkg/staleness` | Documentation staleness checker |
| `pkg/tools` | Tool handlers, graph lifecycle, config |
| `pkg/untested` | Untested code detector |
| `pkg/watcher` | File system watcher for auto-rebuild |
| `pkg/webhook` | Webhook integration |

### Internal Communication

Components communicate via Go channels with typed messages (not MCP for internal coordination). The `integration.Governor` struct wires everything together:

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│   Gateway    │────▶│ ToolHandlers  │────▶│  Components  │
│  (MCP/WS)   │     │  (dispatch)   │     │ (rigour, sar │
└─────────────┘     └──────────────┘     │  if, adr...) │
                                          └──────────────┘
```

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run tests for a specific package
go test ./pkg/rigour/...
go test ./pkg/sarif/...
```

26 packages total, 24 with test files.

## Prometheus Metrics

When `metrics.enabled: true`, Governor exposes:

| Metric | Type | Description |
|--------|------|-------------|
| `governor_build_info` | Gauge | Build version |
| `governor_graph_nodes_total` | Gauge | Call graph node count |
| `governor_graph_edges_total` | Gauge | Call graph edge count |
| `governor_graph_last_build_timestamp_seconds` | Gauge | Last graph build time |
| `governor_tool_calls_total` | Counter | Tool call count by name/status |
| `governor_tool_latency_seconds` | Histogram | Tool call latency |
| `governor_watcher_events_total` | Counter | File watcher events |
| `governor_active_connections` | Gauge | Active WebSocket connections |

## License

Private — see repository owner for access.
