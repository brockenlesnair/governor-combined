---
title: "Local Development Runbook"
owner: "governor-maintainers"
severity: SEV-3
steps:
  - build
  - smoke-test
  - audit
---

# Local Development Runbook

## Purpose

Start Governor locally and verify the MCP endpoints and tool calls behave correctly.

## Prerequisites

- Go 1.25+
- A local checkout of `governor-combined`

## Steps

1. Build the binary.

   ```bash
   go build -o governor ./cmd/governor
   ```

2. Run the smoke tests.

   ```bash
   ./scripts/test-mcp.sh
   ./scripts/test-mcp-tools.sh
   ./scripts/test-mcp-call.sh
   ```

3. If audit output is empty, delete `data/governor.db` and retry.

## Verification

- `initialize` succeeds.
- `tools/list` returns Governor tools.
- `audit_project` returns graph-backed results.

## Rollback

- Delete `data/governor.db` if it is stale.
- Rebuild the binary and rerun the smoke tests.

## Expected Result

- `initialize` succeeds.
- `tools/list` returns Governor tools.
- `audit_project` returns graph-backed results.
