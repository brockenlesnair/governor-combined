# Getting Started with Governor

Governor is an MCP (Model Context Protocol) server that enforces code quality, security, and documentation standards across your codebase. It exposes 15 tools that AI agents can call via the MCP protocol.

## Prerequisites

- Go 1.25+
- (Optional) [ast-grep](https://github.com/ast-grep/ast-grep) CLI for advanced rigour checks

## Build

```bash
git clone https://github.com/brockenlesnair/governor-combined.git
cd governor-combined
go build -o governor ./cmd/governor
```

## Run

### HTTP Mode (default)

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

### Stdio Mode (for MCP integrations)

```bash
./governor --stdio
```

Reads JSON-RPC from stdin, writes responses to stdout.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `governor.yaml` | Path to config file |
| `--stdio` | `false` | Run MCP server over stdio |
| `--port` | `0` | HTTP server port (overrides config) |
| `--project-root` | `""` | Project root directory (overrides config) |

## Configuration

Governor is configured via `governor.yaml`. Key sections:

```yaml
features:
  safety:
    enabled: true
    audit_path: "./data/audit.log"
  callgraph:
    enabled: true
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
  memory:
    enabled: true
    storage_path: "./data/memory.db"
```

## Architecture Overview

Governor has 22 packages organized around these subsystems:

- **Safety** (`pkg/safety`): Pattern detection, policy enforcement, risk scoring
- **Rigour** (`pkg/rigour`): DLP pre-filter, brain pattern learning, fix packet pipeline
- **Callgraph** (`pkg/callgraph`): Tree-sitter based call graph builder and analyzer
- **SARIF** (`pkg/sarif`): SARIF 2.1.0 report generation with deduplication
- **ADR** (`pkg/adr`): Architecture Decision Record management with GitHub integration
- **Memory** (`pkg/memory`): Persistent memory with vector/FTS/graph hybrid search
- **Collaboration** (`pkg/collab`): Shadow files, locking, merge strategies
- **Gateway** (`pkg/gateway`): MCP protocol server, tool registration, dispatch

## Next Steps

- [Safety Guide](safety-guide.md) - Learn about policy enforcement and pattern detection
- [Rigour Guide](rigour-guide.md) - Understand the DLP filter and fix packet pipeline
- [ADR Guide](adr-guide.md) - Set up Architecture Decision Records
- [SARIF Guide](sarif-guide.md) - Export findings as SARIF reports
- [Memory Guide](memory-guide.md) - Use persistent memory with hybrid search
- [Callgraph Guide](callgraph-guide.md) - Analyze code dependencies and impact
- [Collaboration Guide](collaboration-guide.md) - Multi-agent file editing
- [MCP Tools Reference](mcp-tools-reference.md) - Complete API reference for all 15 tools
