# SARIF Guide

Governor exports findings as SARIF 2.1.0 reports for integration with security toolchains. The SARIF system includes parsing, deduplication, rule normalization, and summary statistics.

## Concepts

### SARIF 2.1.0 Format

SARIF (Static Analysis Results Interchange Format) is a standard format for static analysis results:

```json
{
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
  "version": "2.1.0",
  "runs": [{
    "tool": {
      "driver": {
        "name": "governor",
        "version": "1.0.0"
      }
    },
    "results": [...]
  }]
}
```

### Rule ID Normalization

Rule IDs are normalized into components:

| Prefix | Example | Category | Rule |
|--------|---------|----------|------|
| `hangar.dependabot` | `hangar.dependabot.vulnerable-dependency` | - | `vulnerable-dependency` |
| `rigour.ast.complexity` | `rigour.ast.complexity.high-cognitive-load` | - | `high-cognitive-load` |
| `adr.conflict` | `adr.conflict.duplicate` | - | `duplicate` |
| `rb.standards-gap` | `rb.standards-gap.missing-codeowners` | - | `missing-codeowners` |

### Severity Mapping

| SARIF Level | Governor Severity | Weight |
|-------------|-------------------|--------|
| `error` | error | 4 |
| `warning` | warning | 3 |
| `note` | note | 2 |
| - | none | 0 |

## Deduplication

Governor provides three deduplication strategies:

### 1. Fingerprint Dedup

Groups findings by `SHA256(rule_id + file_path + line)` and keeps one per group.

### 2. Time-Window Dedup

Removes findings that appear within a time window (default: 24h) for the same rule+file+line.

### 3. Cross-Tool Dedup

When multiple tools report the same issue, keeps the one from the more trusted tool:

| Priority | Tool |
|----------|------|
| 1 | hangar.dependabot |
| 2 | rigour.ast.complexity |
| 3 | adr.conflict |
| 4 | rb.standards-gap |
| 5 | sarif (unknown) |

### Dedup Pipeline

All three strategies run in sequence:

```
findings → cross-tool dedup → fingerprint dedup → time-window dedup → kept
```

Configuration:
```yaml
features:
  sarif:
    enabled: true
    output_path: "./data/sarif"
    dedup_window: 24h
```

## Finding Structure

```go
type Finding struct {
    ID          string           `json:"id"`
    RuleID      string           `json:"rule_id"`
    Normalized  NormalizedRuleID `json:"normalized_rule"`
    Level       string           `json:"level"`
    Severity    Severity         `json:"severity"`
    Message     string           `json:"message"`
    File        string           `json:"file"`
    Line        int              `json:"line"`
    Column      int              `json:"column"`
    Tool        string           `json:"tool"`
    ToolVersion string           `json:"tool_version"`
    Fingerprint string           `json:"fingerprint"`
    Source      string           `json:"source"`
    Timestamp   time.Time        `json:"timestamp"`
}
```

## Summary Statistics

```go
type ReportSummary struct {
    TotalFindings int                `json:"total_findings"`
    BySeverity    map[Severity]int   `json:"by_severity"`
    ByTool        map[string]int     `json:"by_tool"`
    ByPrefix      map[RulePrefix]int `json:"by_prefix"`
    FilesAffected int                `json:"files_affected"`
    TopRules      []RuleCount        `json:"top_rules"`
}
```

## MCP Tool

### `sarif_export`

Export findings as a SARIF report:

```json
{
  "tool": "sarif_export",
  "arguments": {
    "findings": [
      {
        "rule_id": "GO004",
        "message": "Hardcoded secret detected",
        "file": "config.go",
        "line": 42,
        "level": "error"
      }
    ],
    "source": "safety"
  }
}
```

Response:
```json
{
  "report": "{...SARIF JSON...}",
  "findings": 1,
  "source": "safety"
}
```

## DefectDojo Integration

The `pkg/sarif/defectdojo.go` module provides integration with DefectDojo for vulnerability management.
