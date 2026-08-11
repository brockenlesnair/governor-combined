---
title: "Incident Response Playbook"
triage: "Identify the failure mode."
roles: ["maintainer", "operator"]
escalation: "Escalate when startup or MCP handshake fails."
communication: "Report exact command, timestamp, and response."
diagnosis: "Check cache state and graph rebuild behavior."
mitigation: "Clear stale cache and rerun the smoke tests."
---

# Incident Response Playbook

## Scope

Use this playbook when Governor fails to start, returns malformed MCP payloads, or reports empty graph state.

## Triage

1. Check whether `governor` starts in stdio mode.
2. Run `./scripts/test-mcp.sh`.
3. Run `./scripts/test-mcp-tools.sh`.
4. Run `./scripts/test-mcp-call.sh`.

## Roles

- Maintainer: rebuild and patch the server.
- Operator: rerun the smoke tests and collect output.

## Common Failure Modes

- Empty graph cache in `data/governor.db`
- Wrong project root passed to the server
- Target repository is missing Go module metadata

## Recovery

- Remove `data/governor.db` if it is stale or empty.
- Rebuild Governor.
- Re-run the smoke tests.

## Escalation

- If `initialize` fails, treat it as a startup failure.
- If `initialize` succeeds but `audit_project` is empty, treat it as a graph lifecycle failure.
