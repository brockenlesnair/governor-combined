# Rigour Guide

The Rigour Supervisor is an adaptive code quality coordinator that combines DLP pre-filtering, brain-based pattern learning, fix packet processing, and multi-agent coordination.

## Architecture

```
┌─────────────────────────────────────────────┐
│              RigourSupervisor                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │   DLP    │  │  Brain   │  │Coordinator│  │
│  │  Filter  │  │ (learn)  │  │ (agents) │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  │
│       │              │              │        │
│  ┌────┴─────┐  ┌────┴─────┐  ┌────┴─────┐  │
│  │  State   │  │   Fix    │  │ Template │  │
│  │ Machine  │  │ Applier  │  │  Loader  │  │
│  └──────────┘  └──────────┘  └──────────┘  │
└─────────────────────────────────────────────┘
```

## Components

### 1. DLP Pre-Filter

Scans code text before agent processing for:
- **Secrets**: API keys, tokens, passwords, AWS keys, private keys
- **PII**: Emails, phone numbers, SSNs, credit cards
- **Entropy**: Shannon entropy detection (sliding window, 20 chars)

Configuration:
```yaml
rigour:
  dlp_enabled: true
  entropy_threshold: 5.5  # bits per character
```

False positive exemptions:
```go
dlp.AddFalsePositive("testdata/")
dlp.AddFalsePositive("*_test.go")
```

### 2. Brain (Pattern Learning)

The Brain learns code patterns over time:

- **Patterns** grow strength multiplicatively with reinforcement (logarithmic curve)
- **Hard Rules** are promoted when strength exceeds threshold (default: 0.9)
- **Decay** reduces strength over time (time-weighted)
- **Pruning** removes patterns below threshold (default: 0.1)

```go
// Learn a pattern
brain.Learn(ctx, Pattern{
    ID:          "no-nested-callbacks",
    Name:        "nested_callbacks",
    Description: "Avoid nested callback patterns",
})

// Query strong patterns
patterns, _ := brain.GetStrongPatterns(ctx, 0.7)

// Get hard rules
rules, _ := brain.GetHardRules(ctx)
```

Configuration:
```yaml
rigour:
  brain:
    hard_threshold: 0.9
    decay_rate: 0.1
    prune_threshold: 0.1
```

### 3. State Machine

The supervisor follows a strict lifecycle:

```
idle → working → fixing → done → idle
         ↓         ↓
      handoff    working
         ↓
       idle
```

States:
- `idle`: Waiting for work
- `working`: Agent processing task
- `fixing`: Applying fix packet
- `handoff`: Transferring to another agent
- `done`: Task completed

### 4. Fix Packet Pipeline

Rigour returns human-readable text (not JSON). The `Parser` extracts:

- **Gate name**: The quality gate that triggered (e.g., `cyclomatic_complexity`)
- **Severity**: `info`, `warning`, `error`, `critical`
- **Files**: Target files with optional line ranges
- **Instructions**: Numbered fix steps
- **Constraints**: `do_not_touch`, `max_files`, `paradigm`
- **Verification**: Commands to verify the fix

Example rigour output:
```
Gate: cyclomatic_complexity
Severity: warning
Files: src/handler.go:45-80
1. Extract nested logic into separate functions
2. Reduce branching depth
do_not_touch: vendor/
max_files: 3
verify: go vet ./...
```

### 5. Fix Applier (Strategies)

The `FixApplier` determines how to apply fixes:

| Strategy | When | How |
|----------|------|-----|
| `ast-grep` | Known patterns | Run ast-grep CLI |
| `create_file` | Missing docs | Write template file |
| `delegate` | Unknown patterns | Return context to agent |
| `skip` | No action needed | No-op |

Known ast-grep patterns:
- `cyclomatic_complexity`
- `nested_callbacks`
- `code_duplication`
- `naming_convention`
- `missing_readme`, `missing_contributing`, `missing_license`, `missing_changelog`

### 6. Coordinator (Multi-Agent)

Manages multiple agents working in parallel:

- **Registration**: Agents register with scope (file path patterns)
- **Checkpointing**: Record progress (0.0-1.0) with quality scores
- **Handoff**: Transfer responsibility between agents
- **Overlap detection**: Warns when agents touch overlapping files
- **Drift detection**: EWMA-based score gaming detection

```go
coordinator.Register(ctx, "agent-1", []string{"src/auth/*"})
coordinator.Checkpoint(ctx, "agent-1", &Checkpoint{
    Progress: 0.5,
    Score:    0.85,
    Files:    []string{"src/auth/login.go"},
})
coordinator.Handoff(ctx, "agent-1", "agent-2")
```

## MCP Tools

### `rigour_check`

Run DLP pre-filter on code text:

```json
{
  "tool": "rigour_check",
  "arguments": {
    "text": "api_key = \"sk-1234567890abcdef\"",
    "file_path": "config.go"
  }
}
```

### `rigour_state`

Get current supervisor state:

```json
{"tool": "rigour_state"}
```

### `rigour_stats`

Get brain statistics:

```json
{"tool": "rigour_stats"}
```

Returns: `pattern_count`, `hard_rule_count`, `avg_strength`, `max_strength`
