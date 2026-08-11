---
title: "Governor-combined release notes"
version: "2.0.0"
highlights:
  - MCP smoke-test scripts for initialize, tools/list, and tools/call
  - Governor graph lifecycle now rebuilds empty cached graphs
  - Baseline governance documentation added for docgov
breaking_changes:
  - None
---

# Release Notes

## Highlights

- Added local registration helpers for `mcpmu` and Codex.
- Added smoke tests for startup, tool listing, and real tool calls.
- Fixed the empty graph cache failure that blocked `audit_project`.
- Added a baseline governance documentation set so docgov can report real inventory.

## Breaking Changes

- None.

## Migration

- No migration required for existing users.
- If you have a stale `data/governor.db`, delete it once so the rebuilt graph can be cached cleanly.

## Bug Fixes

- Empty cached graphs are no longer treated as usable analysis state.
- Documentation validation now has a representative document inventory to scan.
