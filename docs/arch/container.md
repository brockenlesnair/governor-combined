---
title: "Governor-combined container diagram"
containers:
  - mcp client
  - governor stdio server
  - filesystem
  - sqlite cache
---

# Container Diagram

## Containers

- **MCP client**: launches Governor and consumes its JSON-RPC output.
- **Governor stdio server**: accepts MCP requests and dispatches tools.
- **Filesystem**: source of truth for the repository being analyzed.
- **SQLite cache**: stores cached graph and operational state in `data/governor.db`.

## Responsibilities

- The MCP client is responsible for orchestrating requests.
- Governor is responsible for analysis, documentation inventory, and governance checks.
- The filesystem is responsible for holding the source tree and docs Governor scans.
- SQLite is responsible for avoiding unnecessary rebuilds when the cache is still valid.

## Data Flow

1. The client starts Governor.
2. Governor scans the repository root.
3. Governor builds or restores its graph cache.
4. Tools such as `audit_project` and `validate_code` operate on the resulting state.
