---
title: "Architecture Context"
context: "Governor operates as a local MCP server over stdio or HTTP."
status: draft
---

# Architecture Context

Governor is a local MCP server that analyzes a repository through a call graph,
safety validation, documentation governance, and supporting analysis tools.

## External Actors

- MCP client
- Filesystem
- Optional GitHub/ADR integrations

## Core Runtime

- `cmd/governor` boots the HTTP or stdio server.
- `pkg/tools` builds and serves graph-backed MCP tools.
- `pkg/gateway` dispatches MCP requests.
- `pkg/callgraph` builds the analysis graph from source files.

## Operational Boundaries

- The stdio server expects a repository root and can run without HTTP.
- The HTTP server exposes `/health`, `/ready`, `/tools`, `/mcp`, and `/metrics`.
- Persistent graph state is cached in `data/governor.db` by default.
