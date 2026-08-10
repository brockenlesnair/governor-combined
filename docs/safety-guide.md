# Safety Guide

The safety system detects dangerous patterns, policy violations, and security issues in source code. It supports Go AST parsing, regex patterns, metavariable patterns, and obfuscation detection.

## Concepts

### Policies

A policy is a named collection of rules with a default action. Governor ships with three built-in policies:

| Policy | Default Action | Description |
|--------|----------------|-------------|
| `default` | warn | Standard security policy |
| `strict` | block | Blocks on high+ severity |
| `permissive` | allow | Warns on critical only |

### Rules

Each rule has:
- **ID**: Unique identifier (e.g., `GO001`)
- **Severity**: `low`, `medium`, `high`, `critical`
- **Action**: `allow`, `warn`, `block`
- **Pattern**: Regex, AST, or metavariable pattern
- **Languages**: Applicable languages (e.g., `["go"]`)
- **Tags**: Classification tags (e.g., `["security", "secrets"]`)

### Built-in Rules

| Rule | Name | Severity | Action | Pattern |
|------|------|----------|--------|---------|
| GO001 | dangerous-exec | high | block | `exec.Command` |
| GO002 | sql-injection | critical | block | `fmt.Sprintf.*(SELECT\|INSERT)...` |
| GO003 | weak-crypto | medium | warn | `md5.New\|sha1.New` |
| GO004 | hardcoded-secret | critical | block | `(password\|secret\|token)...` |
| GO005 | unsafe-pointer | high | block | `unsafe.Pointer` |
| GO006 | dangerous-rm | critical | block | `rm -rf?` |
| GO007 | insecure-tls | high | block | `InsecureSkipVerify: true` |
| GO008 | path-traversal | high | block | `os.Open(.*+.*?)` |
| GO009 | eval-injection | high | block | `exec.Command.*+` |
| GO010 | debug-print | low | warn | `fmt.Println([^"])` |
| GO011 | todo-comment | low | warn | `// TODO` |
| GO012 | ignored-error | medium | warn | `_ = [a-zA-Z]` |

### Pattern Types

1. **Regex**: Standard regular expressions
2. **AST**: Go AST structural matching (detects `func`, `import`, `:=` patterns)
3. **Metavariable**: Patterns with `$VAR` placeholders (converted to regex)

### Suppression

Suppress a rule with a comment:
```go
// governor:allow GO001
result := exec.Command("ls")
```

## Risk Scoring

The `RiskScorer` computes CVSS-like scores:

- **Base score** from severity: critical=9.0, high=6.0, medium=3.5, low=1.0
- **Context multiplier**: production=1.5x, deployment=1.3x, test=0.5x
- **Security tag bonus**: 1.2x for security-tagged rules
- **Aggregate score**: max score + compound risk from multiple findings

Risk levels: `low` (<2.5), `medium` (2.5-5.0), `high` (5.0-8.0), `critical` (>8.0)

## Audit Log

The safety system writes tamper-evident audit logs using SHA-256 chain hashing:

```go
// Each entry hashes the previous entry's hash + current data
hash = SHA256(prevHash + JSON(entry))
```

Verify chain integrity with `AuditLog.VerifyChain()`.

## Configuration

```yaml
features:
  safety:
    enabled: true
    audit_path: "./data/audit.log"
```

## MCP Tool

Use `validate_code` or `validate_diff` to run safety checks:

```json
{
  "tool": "validate_code",
  "arguments": {
    "file": "main.go",
    "policy": "strict"
  }
}
```

Response includes: `passed`, `risk_score`, `risk_level`, `findings`, `blocked`, `reason`.
