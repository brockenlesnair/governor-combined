---
title: "Threat Model"
system: "Governor-combined local MCP server"
threats:
  - spoofing
  - tampering
  - repudiation
  - information disclosure
  - denial of service
  - elevation of privilege
mitigations:
  - scope the server to the intended project root
  - rebuild stale graphs
  - review tool output before acting on it
---

# Threat Model

## Assets

- Source code in the analyzed repository
- Cached graph state in `data/governor.db`
- MCP tool outputs
- Local filesystem contents

## Threats

- Reading or exposing sensitive source files through tool calls
- Empty or stale graph caches causing false confidence
- Running the server against the wrong repository root
- Malformed request payloads producing bad responses

## STRIDE Coverage

- Spoofing: only trusted local clients should register and call the server.
- Tampering: cached state should be rebuilt when it looks stale or empty.
- Repudiation: keep exact command output when reproducing failures.
- Information Disclosure: inspect tool output before sharing it.
- Denial of Service: avoid running heavy audits on the wrong target path.
- Elevation of Privilege: do not broaden filesystem scope unnecessarily.

## Mitigations

- Scope the server to the intended project root.
- Rebuild the graph when cache state is empty or suspect.
- Use the provided smoke tests before trusting results.
- Review tool output before acting on it.
