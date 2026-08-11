---
title: "Service Level Objectives"
slis:
  - initialize latency
  - tools list latency
slo_targets:
  - initialize succeeds for valid roots
  - tools list returns Governor tools
error_budget: "Empty cache failures are zero tolerance"
---

# Service Level Objectives

## SLI

- Time to complete `initialize`
- Time to complete `tools/list`

## SLO Targets

- `initialize` succeeds for valid roots.
- `tools/list` returns Governor tools.

## Error Budget

- Empty cache failures are zero tolerance.

## Burn Rate

- Keep startup deterministic so the burn rate stays low.
