# Governor-Combined: Architecture Deep-Dive

> Comprehensive guide to the internal design, component wiring, message flow, and extensibility of the Governor code quality enforcement platform. Also covers the surrounding workspace ecosystem components: MCP Gateway, Hangar, Integration, and Governance Pipelines.

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Component Map](#component-map)
3. [Startup Flow](#startup-flow)
4. [MCP Tool Dispatch](#mcp-tool-dispatch)
5. [Safety System](#safety-system)
6. [Rigour Supervisor](#rigour-supervisor)
7. [Memory & Persistence](#memory--persistence)
8. [V1-V2 Integration](#v1-v2-integration)
9. [Extensibility](#extensibility)
10. [Message Flow Diagrams](#message-flow-diagrams)
11. [Configuration Reference](#configuration-reference)
12. [Key Design Decisions](#key-design-decisions)
13. [Prometheus Metrics](#prometheus-metrics)
14. [Appendix: Core Design Patterns](#appendix-core-design-patterns)
15. [Appendix: Workspace Project Structure](#appendix-workspace-project-structure)
16. [Appendix: Architecture Layers](#appendix-architecture-layers)
17. [Appendix: Related Workspace Components](#appendix-related-workspace-components)
18. [Appendix: Additional Extensibility Patterns](#appendix-additional-extensibility-patterns)
19. [Appendix: Testing Strategy](#appendix-testing-strategy)
20. [Appendix: Performance Considerations](#appendix-performance-considerations)
21. [Appendix: Security Considerations](#appendix-security-considerations)
22. [Appendix: Package Dependency Graph](#appendix-package-dependency-graph)

---

## System Overview

Governor is a multi-language code quality enforcement platform that exposes its functionality as MCP (Model Context Protocol) tools. An AI coding agent (like Claude, Cursor, or similar) connects to Governor and calls tools like `validate_code`, `search_code`, or `rigour_check` to inspect, analyze, and fix code.

The system solves several problems at once:

- **Static safety**: Pattern-based detection of dangerous code (SQL injection, hardcoded secrets, unsafe pointers) with configurable policy enforcement and audit logging.
- **Call graph analysis**: Builds a full call graph of the codebase, enabling symbol search, caller/callee traversal, impact analysis, and dead code detection.
- **Learning system (Rigour)**: A DLP pre-filter catches secrets and PII before agent processing. Patterns strengthen with reinforcement; strong patterns become hard rules. Fix packets describe code issues in a TEXT format that the system parses and applies via ast-grep, file creation, or delegation to agents.
- **Document governance**: Tracks markdown/docs freshness, validates document quality, and watches for changes.
- **Multi-agent coordination**: File locking, shadow documents, and merge conflict resolution for multiple agents editing the same codebase.
- **Observability**: Prometheus metrics, tamper-evident audit logs, and SARIF export for CI integration.

Governor runs as either an HTTP server (default, WebSocket at `/mcp`) or a stdio-based MCP server (flag `--stdio`). It serves 15 MCP tools plus 2 built-in gateway tools.

---

## Component Map

The codebase lives under `github.com/brockenlesnair/governor-combined` and contains 22 packages:

```
governor-combined/
├── cmd/governor/          # Entry point (main.go)
├── config/                # YAML configuration loading
├── pkg/
│   ├── adr/               # Architecture Decision Records (V1 port)
│   ├── callgraph/         # Call graph builder, graph algorithms
│   ├── collab/            # Multi-agent file locking & shadow merges
│   ├── deadcode/          # Dead code detection
│   ├── docgov/            # Document governance & staleness tracking
│   ├── gateway/           # HTTP/WebSocket gateway + tool dispatch
│   ├── hangar/            # Fleet compliance scorecard client (V1 port)
│   ├── httpproxy/         # HTTP proxy for outbound requests
│   ├── integration/       # Wires all V1 components together
│   ├── mcp/               # ToolRegistry, ToolHandler, ToolDefinition
│   ├── memory/            # Hybrid vector/FTS/graph memory store (SQLite)
│   ├── persist/           # SQLite persistence for call graph cache
│   ├── repo/              # Repo Butler client (V1 port)
│   ├── rigour/            # Learning system: brain, DLP, state machine, fix packets
│   ├── safety/            # Safety validator: patterns, risk scoring, audit
│   ├── sarif/             # SARIF 2.1.0 report generation & dedup
│   ├── search/            # Fuzzy/regex/type-aware code search
│   ├── staleness/         # Document staleness checker
│   ├── tools/             # MCP tool handlers + GraphLifecycle
│   ├── untested/          # Untested code detection
│   ├── watcher/           # Filesystem polling watcher
│   └── webhook/           # Outbound webhook client with circuit breaker
```

### Package Relationships

```
                    ┌─────────────────┐
                    │  cmd/governor    │
                    │  (main.go)      │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
        ┌──────────┐  ┌──────────┐  ┌──────────────┐
        │  config  │  │ gateway  │  │ integration  │
        └──────────┘  └────┬─────┘  └──────┬───────┘
                           │               │
                    ┌──────┴──────┐  ┌──────┴──────────┐
                    │ mcp.ToolReg │  │ Governor struct  │
                    └──────┬──────┘  └──────┬──────────┘
                           │               │
                    ┌──────┴──────┐  ┌──────┼──────────────┐
                    │    tools    │  │      │              │
                    │ (handlers)  │  ▼      ▼              ▼
                    └──────┬──────┘ safety rigour       sarif
                           │              │              │
              ┌────────────┼──────┐       │         ┌────┴────┐
              ▼            ▼      ▼       ▼         │ dedup   │
         callgraph     search  deadcode  brain      └─────────┘
              │                      untested
         ┌────┴────┐
         │ analyzer│
         └─────────┘
```

### Dependency Layers

| Layer | Packages | Role |
|-------|----------|------|
| Entry | `cmd/governor`, `config` | CLI parsing, config loading |
| Transport | `gateway`, `mcp` | HTTP/WS server, tool registry |
| Tools | `tools` | Tool handler implementations |
| V2 Core | `callgraph`, `search`, `deadcode`, `untested`, `safety` | Graph analysis, code quality |
| V1 Ports | `rigour`, `sarif`, `adr`, `hangar`, `repo` | Learning, ADR, fleet compliance |
| Infrastructure | `memory`, `persist`, `watcher`, `webhook`, `collab`, `docgov` | Storage, events, coordination |
| Glue | `integration` | Wires V1 components together |

---

## Startup Flow

When `governor serve` runs, the startup sequence is:

### Step 1: Configuration (`config/config.go`)

```go
cfg, err := config.LoadConfig(*configPath)
```

`LoadConfig` reads a YAML file, overlays it on `DefaultConfig()`, and returns a `GovernorConfig` with 13 feature sections: Search, Untested, Deadcode, Webhook, HTTPProxy, Memory, Safety, Callgraph, Persist, Gateway, Metrics, DocGov, and the always-on Rigour/SARIF/ADR.

### Step 2: Tools Setup (`pkg/tools/`)

```go
gl, err := tools.NewGraphLifecycle(toolsCfg, logger)
th, err := tools.NewToolHandlers(gl, logger)
```

`GraphLifecycle` manages the call graph's lifecycle: build, cache to SQLite, rebuild on file changes. On startup it attempts to load a cached graph from `persist.SQLiteStore`. `ToolHandlers` creates the safety validator and holds references to all detector instances (searcher, untested, deadcode, analyzer).

### Step 3: Extended Components (`pkg/integration/wiring.go`)

```go
govCfg := integration.DefaultConfig()
governor, err := integration.New(logger, govCfg)
th.SetExtendedComponents(governor.Rigour, governor.SARIF, governor.ADRClient, governor.Hangar, governor.Repo)
```

`integration.New` constructs the `Governor` struct:

```go
type Governor struct {
    Safety   *safety.SafetyValidator
    Rigour   *rigour.RigourSupervisor
    SARIF    *sarif.DedupPipeline
    ADRHook  *adr.CIHookHandler
    ADRCmd   *adr.CommandHandler
    ADRClient adr.AdrMcpClient
    Hangar   *hangar.Client
    Repo     *repo.Client
    Logger   *slog.Logger
}
```

Components that need external services (Hangar, Repo) are optional: they're skipped if their config is incomplete.

### Step 4: Gateway & Tool Registration

```go
gw := gateway.NewGateway(gwCfg, logger)
th.RegisterToolsOnGateway(gw)
```

The gateway wraps a `mcp.ToolRegistry` and handles both legacy handler maps and registry-based dispatch. `RegisterToolsOnGateway` registers all 15 tools (plus 2 built-in tools the gateway adds: `health_check` and `list_tools`).

### Step 5: Document Governance (optional)

If `cfg.Features.DocGov.Enabled`, Governor creates a `DocumentRegistry`, a staleness checker, and optionally starts a `watcher.Watcher` that refreshes documents on change.

### Step 6: Background Systems

- `governor.Start(ctx)` starts the Rigour supervisor and connects to Repo Butler.
- A file `watcher.Watcher` polls the project root; on file changes it calls `gl.RebuildGraphAsync(ctx)` to trigger an async graph rebuild.
- `gl.StartRebuildLoop(ctx)` processes rebuild requests from a channel and does periodic rebuilds.

### Step 7: Server

Either `runStdioServer` (reads JSON-RPC from stdin, dispatches via gateway) or `runHTTPServer` (listens on configured port, serves `/health`, `/ready`, `/tools`, `/metrics`, and `/mcp` WebSocket).

---

## MCP Tool Dispatch

### The Pattern

Every tool follows the same pattern in `pkg/tools/handlers.go`:

1. A `ToolDefinition` with name, description, and JSON Schema input.
2. A handler function: `func (th *ToolHandlers) handleXxx(ctx context.Context, args map[string]any) (map[string]any, error)`
3. Registration in a slice of `{def, handler}` pairs.

```go
// Registration pattern
tools := []struct {
    def    mcp.ToolDefinition
    handler mcp.ToolHandler
}{
    {
        def: mcp.ToolDefinition{
            Name:        "validate_code",
            Description: "Validate a source file for dangerous patterns...",
            InputSchema: map[string]any{...},
        },
        handler: th.handleValidateCode,
    },
    // ... 14 more tools
}
for _, t := range tools {
    gw.RegisterTool(t.def, t.handler)
}
```

### The 15 MCP Tools

| Tool | Handler | Domain |
|------|---------|--------|
| `validate_code` | `handleValidateCode` | Safety: validate a file against policy rules |
| `validate_diff` | `handleValidateDiff` | Safety: validate only changed lines in a diff |
| `search_code` | `handleSearchCode` | Call graph: fuzzy/regex/type-aware symbol search |
| `get_callers` | `handleGetCallers` | Call graph: direct or transitive callers |
| `get_callees` | `handleGetCallees` | Call graph: direct or transitive callees |
| `get_impact` | `handleGetImpact` | Call graph: impact analysis for a function |
| `audit_project` | `handleAuditProject` | Combined: untested + deadcode + safety audit |
| `rigour_check` | `handleRigourCheck` | Rigour: DLP pre-filter scan |
| `rigour_state` | `handleRigourState` | Rigour: current supervisor state |
| `rigour_stats` | `handleRigourStats` | Rigour: brain pattern statistics |
| `sarif_export` | `handleSARIFExport` | SARIF: export findings as SARIF 2.1.0 report |
| `adr_create` | `handleADRCreate` | ADR: create architecture decision record |
| `adr_list` | `handleADRList` | ADR: list existing records |
| `hangar_score` | `handleHangarScore` | Hangar: fleet compliance scorecard |
| `repo_health` | `handleRepoHealth` | Repo Butler: repository health tier |

Plus 2 built-in gateway tools: `health_check` and `list_tools`.

### Dispatch Flow

```
Client (AI agent)
    │
    ▼
JSON-RPC request (WebSocket or stdin)
    │
    ▼
gateway.ExecuteTool(ctx, name, args)
    │
    ├── toolDispatchUseRegistry == true
    │       └── mcp.ToolRegistry.Execute(ctx, name, args)
    │               └── handler(ctx, args)  [ToolHandler function]
    │
    └── toolDispatchUseRegistry == false
            └── legacyHandlers[name](ctx, args)
```

The `mcp.ToolRegistry` is a thread-safe map of `name -> ToolHandler`. It also holds `ToolDefinition` objects that are returned by `ListDefinitions()` for client discovery.

---

## Safety System

The safety system (`pkg/safety/`) is a policy-based code validation engine. It parses Go source files into ASTs, applies pattern-matching rules, computes CVSS-like risk scores, and maintains a tamper-evident audit log.

### Core Types

```go
// From pkg/safety/types.go
type SafetyValidator struct {
    config         *Config
    policies       map[string]*Policy
    auditLog       *AuditLog
    obfuscDetector *ObfuscationDetector
    riskScorer     *RiskScorer
}

type Finding struct {
    ID, RuleID     string
    Severity       Severity     // SeverityLow..SeverityCritical
    Action         ActionType   // ActionAllow, ActionWarn, ActionBlock
    File           string
    Line, Column   int
    Message        string
    RiskScore      float64
    Suppressed     bool         // via // governor:allow comment
}

type Policy struct {
    Name          string
    Rules         []*Rule
    DefaultAction ActionType
}
```

### Built-in Rules

12 built-in Go security rules defined in `pkg/safety/patterns.go`:

| ID | Name | Severity | Action |
|----|------|----------|--------|
| GO001 | dangerous-exec | High | Block |
| GO002 | sql-injection | Critical | Block |
| GO003 | weak-crypto | Medium | Warn |
| GO004 | hardcoded-secret | Critical | Block |
| GO005 | unsafe-pointer | High | Block |
| GO006 | dangerous-rm | Critical | Block |
| GO007 | insecure-tls | High | Block |
| GO008 | path-traversal | High | Block |
| GO009 | eval-injection | High | Block |
| GO010 | debug-print | Low | Warn |
| GO011 | todo-comment | Low | Warn |
| GO012 | ignored-error | Medium | Warn |

Three built-in policies: `default` (warn on violations), `strict` (block on high+), `permissive` (allow on critical only).

### Pattern Matching

Rules use three pattern types:

- **Regex** (`PatternTypeRegex`): Standard Go regex against source text.
- **AST** (`PatternTypeAST`): Structural matching against parsed Go AST nodes.
- **Metavariable** (`PatternTypeMetavariable`): `$VAR` placeholders that capture identifiers, converted to regex.

The `CompilePattern` function auto-detects the type from the pattern string.

### Risk Scoring

`RiskScorer` computes CVSS-like scores:

1. Base score from severity (Critical=9.0, High=6.0, Medium=3.5, Low=1.0).
2. Multiplied by context: production (1.5x), deployment (1.3x), test (0.5x), security-tagged rules (1.2x).
3. Aggregate risk uses the max finding score plus compound risk from multiple findings.
4. Final score clamped to [0, 10] and categorized as low/medium/high/critical.

### Suppression

Findings can be suppressed with `// governor:allow` comments (optionally followed by a rule ID). The validator checks up to 5 lines above the finding.

### Audit Log

`AuditLog` writes chain-hashed entries (`AuditEntry` with `Hash` and `PrevHash`) to a file, making it tamper-evident. Each entry captures the full request and result.

### Validation Flow

```
validate_code request
    │
    ▼
SafetyValidator.Validate(ctx, ValidationRequest)
    │
    ├── LoadPolicy("default")
    ├── ParseDiff() or use FileInput list
    ├── For each file:
    │       ├── parser.ParseFile() -> AST
    │       ├── For each Rule in Policy:
    │       │       ├── CompilePattern(rule.Pattern)
    │       │       ├── MatchPattern(pattern, AST, fset)
    │       │       ├── Check isSuppressed() via governor:allow
    │       │       └── ComputeRuleRisk(rule, ChangeContext)
    │       └── ObfuscationDetector.Detect(AST, fset, file)
    │
    ├── RiskScorer.ComputeAggregateRisk(findings, ctx)
    ├── shouldBlock(findings, policy)
    └── AuditLog.Log(req, result)
```

---

## Rigour Supervisor

The rigour system (`pkg/rigour/`) is the learning and auto-fix layer. It coordinates DLP scanning, pattern learning, state management, and fix application.

### Components

```go
// From pkg/rigour/supervisor.go
type RigourSupervisor struct {
    state       *StateMachine
    dlp         *DLPFilter
    coordinator *Coordinator
    brain       *Brain
    parser      *Parser
    fixApplier  *FixApplier
}
```

### DLP Pre-Filter (`dlp.go`)

The DLP filter runs before agents process input. It detects:

- **Secrets**: API keys, tokens, passwords, AWS keys, private keys (7 regex patterns).
- **PII**: Email addresses, phone numbers, SSNs, credit card numbers (4 regex patterns).
- **High entropy**: Shannon entropy > 4.5 bits/character in sliding windows of 20 characters. Extremely high entropy (>5.5) triggers a block.

False positives can be exempted by path pattern (e.g., test fixtures). Results include `Blocked`, `Reasons`, `EntropyHits`, `SecretHits`, and `PIIHits`.

### Brain (`brain.go`)

The Brain stores learned patterns and hard rules:

```go
type Pattern struct {
    ID, Name, Description string
    Strength              float64    // 0.0 - 1.0
    Decay                 float64    // decay rate per cycle
    LastSeen              time.Time
}

type HardRule struct {
    PatternID string
    Rule      string
    Threshold float64
}
```

**Learning cycle**:

1. `Learn(ctx, pattern)`: Reinforces an existing pattern (logarithmic growth: `newStrength = current + 0.1 * (1 - current)`) or creates a new one at strength 0.1.
2. `Decay(ctx)`: Time-weighted decay reduces strength. Patterns below `PruneThreshold` (0.1) are deleted.
3. When strength exceeds `HardThreshold` (0.9), the pattern is promoted to a `HardRule`.

`MatchRules(ctx, filePaths)` returns hard rules relevant to given file paths via extension matching and keyword extraction.

### State Machine (`state_machine.go`)

Five states with strict transition rules:

```
    ┌──────────────────────────────────────────────┐
    │                                              │
    ▼                                              │
  IDLE ──────► WORKING ──────► FIXING             │
                │    │            │                │
                │    ▼            │                │
                │  HANDOFF ──────┤                │
                │    │           │                │
                │    ▼           ▼                │
                └──► DONE ──────┘                 │
                     │                            │
                     └────────────────────────────┘
```

Valid transitions:

| From | To |
|------|----|
| Idle | Working |
| Working | Fixing, Handoff, Done, Idle |
| Fixing | Working, Done, Handoff |
| Handoff | Idle, Working |
| Done | Idle, Working |

The state machine records a history of transitions (up to 256 entries) and fires `TransitionHook` callbacks for observability.

### Fix Packet Pipeline (`fix_packet.go`, `strategy.go`)

Rigour output is TEXT format (not JSON). The `Parser` extracts:

```go
type FixPacket struct {
    GateName     string
    Severity     Severity        // info, warning, error, critical
    Files        []FileTarget    // paths with optional line ranges
    Instructions []string        // numbered steps
    Constraints  Constraints     // DoNotTouch, MaxFiles, Paradigm
    Verification []string        // post-fix verification commands
}
```

The `FixApplier` determines strategy:

1. **AST-grep** (`StrategyASTGre`): For known patterns (cyclomatic complexity, nested callbacks, code duplication, naming conventions). Runs `ast-grep` with registered patterns.
2. **Create File** (`StrategyCreateFile`): For missing documentation (README, CONTRIBUTING, LICENSE, CHANGELOG, docs/). Creates files from templates.
3. **Delegate** (`StrategyDelegate`): For unknown patterns. Builds a context document and delegates to the registered agent.
4. **Skip** (`StrategySkip`): No action needed.

### Coordinator (`coordinator.go`)

Manages multi-agent coordination:

- **Registration**: Agents register with a scope (file path patterns).
- **Checkpointing**: Progress, files touched, quality score at checkpoint.
- **Handoff**: Transfer responsibility between agents.
- **Overlap detection**: Warns when two agents' scopes overlap.
- **Drift detection**: EWMA (Exponentially Weighted Moving Average) tracks quality scores. If a score deviates significantly from the EWMA (>0.15 threshold after 5+ samples), a `DriftAlert` fires.

### Fix Packet Flow

```
rigour_check request (text + file_path)
    │
    ▼
RigourSupervisor.ProcessInput(ctx, text, filePath)
    │
    ├── DLPFilter.Check(ctx, text, filePath)
    │       ├── detectEntropy(text)     -> EntropyHit[]
    │       ├── detectSecrets(text)     -> SecretHit[]
    │       └── detectPII(text)         -> PIIHit[]
    │
    └── return DLPResult{Blocked, Reasons, ...}
```

For fix packet processing:

```
HandleFixPacket(ctx, rawText, agentID)
    │
    ├── Parser.Parse(rawText)           -> FixPacket
    ├── StateMachine.Transition(Working)
    ├── Brain.MatchRules(ctx, filePaths) -> HardRule[]
    ├── StateMachine.Transition(Fixing)
    ├── FixApplier.ApplyFix(ctx, fp, agentID) -> FixResult
    │       ├── DetermineStrategy(fp)
    │       ├── enforceConstraints(fp)
    │       └── applyASTGre / applyCreateFile / applyDelegate
    ├── Brain.Learn(ctx, pattern)       [if ast-grep success]
    ├── StateMachine.Transition(Done)
    └── StateMachine.Transition(Idle)
```

---

## Memory & Persistence

### Memory System (`pkg/memory/`)

A hybrid vector/FTS/graph memory store backed by SQLite:

```go
type Memory struct {
    db                *sql.DB
    entityStore       *EntityStore
    relationshipStore *RelationshipStore
    searchEngine      *SearchEngine       // hybrid vector + FTS
    decayEngine       *DecayEngine
    embeddingEngine   *EmbeddingEngine    // 384-dim float32
}
```

**Entity types**: decision, lesson, fact, preference, person, project, bug, pattern, context.

**Search**: Hybrid retrieval combining vector similarity, full-text search, and graph traversal (spreading activation with configurable `MaxHops`).

**Decay**: Periodic decay passes reduce `DecayScore` (1.0=fresh, 0.0=forgotten) based on access count and age. Forgotten entities are pruned.

**Relationships**: Typed edges between entities (`depends_on`, `decided_by`, `related_to`) with weights. Graph traversal follows outgoing edges via DFS.

### Persistence Layer (`pkg/persist/`)

SQLite-based storage for the call graph cache. `SQLiteStore` serializes `callgraph.Graph` nodes and edges to/from the database, enabling fast startup by loading cached graphs instead of re-parsing.

### Collab Locks (`pkg/collab/`)

File locks are managed via JSON lock files in `.governor/locks/`:

```go
type LockInfo struct {
    Path       string
    AgentID    AgentID
    AcquiredAt time.Time
    ExpiresAt  time.Time
}
```

Locks expire after a configurable timeout (default 5 minutes). The same agent can extend its lock. Expired locks are cleaned up automatically. `ShadowDoc` objects in `.governor/shadows/` hold temporary copies for merge conflict resolution.

---

## V1-V2 Integration

The codebase merges two origins:

- **V2 (core)**: `callgraph`, `search`, `deadcode`, `untested`, `safety`, `memory`, `persist`, `watcher`, `webhook`, `collab`, `docgov`, `staleness`, `httpproxy`, `mcp`, `gateway`, `tools`, `config`.
- **V1 (ports)**: `rigour`, `sarif`, `adr`, `hangar`, `repo`.

### Shared Interfaces

The integration point is `pkg/integration/wiring.go`. The `Governor` struct holds all V1 components and injects them into `ToolHandlers` via `SetExtendedComponents`:

```go
th.SetExtendedComponents(
    governor.Rigour,     // *rigour.RigourSupervisor
    governor.SARIF,      // *sarif.DedupPipeline
    governor.ADRClient,  // adr.AdrMcpClient (interface)
    governor.Hangar,     // *hangar.Client
    governor.Repo,       // *repo.Client
)
```

The `adr.AdrMcpClient` is an interface, allowing a stub implementation (`GoStubAdrMcpClient`) when the real ADR service isn't available. This is the cleanest integration boundary.

### Optional Component Pattern

Hangar and Repo Butler use the optional component pattern:

```go
// Skip if config is incomplete
if cfg.Hangar.BaseURL != "" {
    hangarClient, err := hangar.NewClient(cfg.Hangar, logger)
    if err != nil {
        logger.Warn("hangar client init failed (optional)", "err", err)
    } else {
        g.Hangar = hangarClient
    }
}
```

Tool handlers check for nil clients and return "not configured" errors.

---

## Extensibility

### Adding a New MCP Tool

1. **Define the handler** in `pkg/tools/handlers.go`:

```go
func (th *ToolHandlers) handleMyNewTool(ctx context.Context, args map[string]any) (map[string]any, error) {
    // Extract args
    input, ok := args["input"].(string)
    if !ok || input == "" {
        return nil, fmt.Errorf("input parameter required")
    }

    // Call your component
    result, err := myComponent.DoSomething(ctx, input)
    if err != nil {
        return nil, err
    }

    // Return structured result
    return map[string]any{
        "result": result,
    }, nil
}
```

2. **Add to the tool list** in `RegisterToolsOnGateway`:

```go
{
    def: mcp.ToolDefinition{
        Name:        "my_new_tool",
        Description: "Does something useful",
        InputSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "input": map[string]any{
                    "type":        "string",
                    "description": "The input to process",
                },
            },
            "required": []string{"input"},
        },
    },
    handler: th.handleMyNewTool,
},
```

3. **If the tool needs a new component**, add it to `ToolHandlers` struct, initialize in `NewToolHandlers`, and inject via `SetExtendedComponents` if it comes from a V1-style package.

### Adding a New Safety Rule

Add a rule to `builtinRules` in `pkg/safety/patterns.go`:

```go
{
    ID:          "GO013",
    Name:        "my-new-rule",
    Description: "Detects something dangerous",
    Severity:    SeverityHigh,
    Action:      ActionBlock,
    Pattern:     `my\.dangerous\.pattern`,
    Languages:   []string{"go"},
    Tags:        []string{"security", "my-category"},
    Enabled:     true,
},
```

Or load custom rules via `Config.CustomRules` at startup. Rules can use regex, AST, or metavariable patterns.

### Adding Language Support to the Call Graph

The call graph uses a `SourceParser` interface:

```go
type SourceParser interface {
    ParsePackages(ctx context.Context, rootPath string) ([]*PackageInfo, error)
    ParseFile(ctx context.Context, filePath string, pkgInfo *PackageInfo) ([]*Node, []*Edge, error)
}
```

The default implementation is `GoParser`. To add a new language:

1. Create a new file `pkg/callgraph/parser_python.go` (or similar).
2. Implement `SourceParser` with language-specific AST parsing.
3. Modify `GraphBuilder.Build` to select the parser based on file extension, or register a tree-sitter parser in `pkg/callgraph/treesitter/`.

### Adding a New Rigour Fix Pattern

Register a new ast-grep pattern in `FixApplier.registerKnownPatterns`:

```go
fa.astGrepPatterns["my_gate_name"] = `
rule:
  pattern: $MY_PATTERN
  kind: expression
fix:
  action: replace
  replacement: $BETTER_PATTERN
`
```

And add a corresponding `FixTemplate` YAML file in `pkg/rigour/templates/`.

---

## Message Flow Diagrams

### Request Flow: validate_code

```
┌─────────┐     JSON-RPC      ┌─────────┐    ToolHandler    ┌─────────────────┐
│ AI Agent │ ────────────────► │ Gateway │ ────────────────► │ SafetyValidator │
└─────────┘                    └─────────┘                    └────────┬────────┘
                                                                     │
                                                            ┌────────┴────────┐
                                                            │                 │
                                                       ┌────▼────┐    ┌───────▼───────┐
                                                       │ Parser  │    │ RiskScorer    │
                                                       │ (Go AST)│    │ (CVSS-like)   │
                                                       └────┬────┘    └───────┬───────┘
                                                            │                 │
                                                       ┌────▼─────────────────▼────┐
                                                       │     AuditLog             │
                                                       │  (chain-hashed entries)  │
                                                       └──────────────────────────┘
```

### Request Flow: search_code

```
┌─────────┐     JSON-RPC      ┌─────────┐    ToolHandler    ┌────────────────┐
│ AI Agent │ ────────────────► │ Gateway │ ────────────────► │ GraphLifecycle │
└─────────┘                    └─────────┘                    └───────┬────────┘
                                                                     │
                                                            ┌────────▼────────┐
                                                            │ callgraph.Graph │
                                                            │ (nodes + edges) │
                                                            └────────┬────────┘
                                                                     │
                                                            ┌────────▼────────┐
                                                            │ Searcher        │
                                                            │ (fuzzy/regex/   │
                                                            │  type-aware)    │
                                                            └─────────────────┘
```

### Request Flow: rigour_check

```
┌─────────┐     JSON-RPC      ┌─────────┐    ToolHandler    ┌──────────────────┐
│ AI Agent │ ────────────────► │ Gateway │ ────────────────► │ RigourSupervisor │
└─────────┘                    └─────────┘                    └────────┬─────────┘
                                                                     │
                                                            ┌────────▼────────┐
                                                            │ DLPFilter       │
                                                            │ ┌─────────────┐ │
                                                            │ │ Entropy     │ │
                                                            │ │ Secrets     │ │
                                                            │ │ PII         │ │
                                                            │ └─────────────┘ │
                                                            └─────────────────┘
```

### Fix Packet Pipeline

```
┌──────────┐   rawText    ┌──────────────────┐
│ Agent/   │ ────────────►│ RigourSupervisor  │
│ External │              │ .HandleFixPacket()│
└──────────┘              └────────┬─────────┘
                                  │
                    ┌─────────────▼──────────────┐
                    │ 1. Parser.Parse(rawText)    │
                    │    -> FixPacket             │
                    ├─────────────────────────────┤
                    │ 2. State: Working           │
                    ├─────────────────────────────┤
                    │ 3. Brain.MatchRules(files)  │
                    │    -> HardRule[]            │
                    ├─────────────────────────────┤
                    │ 4. State: Fixing            │
                    ├─────────────────────────────┤
                    │ 5. FixApplier.ApplyFix()    │
                    │    ├── StrategyASTGre?      │
                    │    ├── StrategyCreateFile?  │
                    │    ├── StrategyDelegate?    │
                    │    └── StrategySkip?        │
                    ├─────────────────────────────┤
                    │ 6. Brain.Learn(pattern)     │
                    ├─────────────────────────────┤
                    │ 7. State: Done -> Idle      │
                    └─────────────────────────────┘
```

### File Watcher + Graph Rebuild

```
┌────────────────┐   poll    ┌────────────────┐
│ watcher.Watcher│ ◄───────► │ Filesystem     │
└───────┬────────┘           └────────────────┘
        │ FileEvent
        ▼
┌──────────────────────────────────────────────────────┐
│ Event loop in main.go:                               │
│   case EventModified/EventCreated/EventDeleted:      │
│       gl.RebuildGraphAsync(ctx)                      │
└───────────────────┬──────────────────────────────────┘
                    │ (non-blocking channel send)
                    ▼
┌──────────────────────────────────────────────────────┐
│ GraphLifecycle.StartRebuildLoop():                   │
│   <-rebuildCh -> BuildGraph(ctx)                     │
│   <-ticker.C   -> BuildGraph(ctx)  [periodic]        │
└──────────────────────────────────────────────────────┘
                    │
                    ▼
┌──────────────────────────────────────────────────────┐
│ GraphBuilder.Build(ctx, rootPath):                   │
│   parser.ParsePackages() -> PackageInfo[]            │
│   parser.ParseFile()     -> Node[], Edge[]           │
│   resolveInterfaceCalls()                            │
│   analyzer.Analyze(graph)                            │
└──────────────────────────────────────────────────────┘
```

### WebSocket MCP Transport

```
┌─────────┐   WS frame    ┌──────────────┐    ExecuteTool    ┌──────────┐
│ Client  │ ◄────────────► │ WSClient     │ ────────────────► │ Gateway  │
│ (agent) │                │ .readPump()  │                   │          │
│         │                │ .writePump() │ ◄──────────────── │          │
└─────────┘                └──────────────┘   tool result     └──────────┘

WSClient handles:
  tools/call  -> gateway.ExecuteTool(toolName, args)
  tools/list  -> gateway.ListTools()
```

---

## Configuration Reference

The `config.GovernorConfig` struct maps to `governor.yaml`:

```yaml
features:
  search:
    enabled: true
    max_results: 50
    fuzzy_threshold: 0.3
  untested:
    enabled: true
    include_exported: true
    min_priority: 0.3
  deadcode:
    enabled: true
    exclude_exported: true
    min_confidence: 0.5
  webhook:
    enabled: true
    timeout: 30s
    max_retries: 3
  memory:
    enabled: true
    storage_path: ./data/memory.db
  safety:
    enabled: true
    audit_path: ./data/audit.log
  callgraph:
    enabled: true
  persist:
    enabled: true
    db_path: ./data/governor.db
  gateway:
    tool_dispatch_use_registry: true
    listen: ":8080"
  metrics:
    enabled: true
    path: /metrics
  docgov:
    enabled: true
    project_root: "."
    enable_watcher: true
    watcher_interval: 30s
```

---

## Key Design Decisions

1. **TEXT format for rigour output**: Fix packets use human-readable TEXT, not JSON. This makes them easier for agents to produce and humans to read. The `Parser` uses regex to extract structured data.

2. **Two dispatch paths in Gateway**: The `toolDispatchUseRegistry` flag allows falling back to a legacy handler map during migration.

3. **Optional components via nil checks**: Hangar and Repo Butler are optional. Tool handlers check `th.hangarClient == nil` and return descriptive errors, keeping the core functional without external services.

4. **State machine with hook-based observability**: The rigour state machine fires `TransitionHook` callbacks, allowing logging and metrics without coupling the state logic to observability.

5. **SQLite for everything**: Call graph cache, memory system, and audit logs all use SQLite. The memory system uses WAL mode and a single connection for thread safety.

6. **Bolt-in-memory brain**: The rigour Brain holds patterns in a Go map with mutex protection. No external database. Decay runs periodically and prunes weak patterns. This is fine for single-server deployments but would need externalizing for distributed setups.

7. **Embed.FS for templates**: Rigour fix templates are embedded in the binary via `//go:embed templates/*.yaml`, with optional external override paths.

---

## Prometheus Metrics

Registered in `cmd/governor/main.go`:

| Metric | Type | Description |
|--------|------|-------------|
| `governor_build_info` | GaugeVec | Build version |
| `governor_graph_nodes_total` | Gauge | Call graph node count |
| `governor_graph_edges_total` | Gauge | Call graph edge count |
| `governor_graph_last_build_timestamp_seconds` | Gauge | Last graph build time |
| `governor_tool_calls_total` | CounterVec | Tool calls by name and status |
| `governor_tool_latency_seconds` | HistogramVec | Tool call latency by name |
| `governor_watcher_events_total` | CounterVec | File watcher events by type |
| `governor_active_connections` | Gauge | Active WebSocket connections |

---

*Last updated: August 2026*

---

## Appendix: Core Design Patterns

The following patterns appear throughout the codebase. Each is a standard Go idiom applied to a specific governance concern.

### 1. Plugin/Registry Pattern (Primary Architecture)

The central architecture is a **Service Locator + Strategy pattern**:

```go
// pkg/mcp/types.go
type ToolHandler func(ctx context.Context, args map[string]any) (map[string]any, error)

type ToolRegistry struct {
    tools map[string]ToolDefinition
    mu    sync.RWMutex
}

type ToolDefinition struct {
    Name        string
    Description string
    InputSchema map[string]any
    Handler     ToolHandler
    Mode        ToolMode  // ToolModeInProgress, ToolModeComplete, etc.
}
```

Every feature is registered as a tool via `ToolDefinition` + `ToolHandler`. The Gateway dispatches tool calls through the registry.

### 2. Feature Flag Pattern

Configuration uses 12+ feature configs, each with an `Enabled` bool:

```go
// config/config.go
type GovernorConfig struct {
    Features struct {
        Search   SearchConfig   `yaml:"search"`
        Safety   SafetyConfig   `yaml:"safety"`
        Callgraph CallgraphConfig `yaml:"callgraph"`
        Memory   MemoryConfig   `yaml:"memory"`
        // ... 12+ feature configs
    } `yaml:"features"`
}
```

`DefaultConfig()` provides sensible defaults; YAML overlay allows overrides. The main entry point checks `cfg.Features.X.Enabled` before initializing each subsystem.

### 3. Lifecycle/State Machine Pattern

Multiple components use explicit state machines:

- **GraphLifecycle**: Manages call graph through states: `build -> cache -> serve -> rebuild`
- **RigourSupervisor**: 5-state lifecycle: `IDLE -> WORKING -> FIXING -> HANDOFF -> DONE`
- **CircuitBreaker**: Classic 3-state machine: `Closed -> Open -> HalfOpen`
- **ProcessSupervisor**: `Stopped -> Starting -> Running -> Stopping -> Failed`

### 4. Observer/Pub-Sub Pattern

```go
// pkg/watcher/watcher.go
type Watcher struct {
    events     chan FileEvent
    subscribers []chan<- FileEvent
}

func (w *WatchedFile) Subscribe() <-chan FileEvent {
    ch := make(chan FileEvent, 16)
    w.subscribers = append(w.subscribers, ch)
    return ch
}
```

The `watcher.Watcher` uses channels for event broadcasting. The `docgov` watcher subscribes to file changes and refreshes documents.

### 5. Strategy Pattern

- **SourceParser** interface: `callgraph.SourceParser` allows swapping parsers (GoParser vs tree-sitter parsers)
- **ConflictStrategy** in collab: Different merge strategies (last-write-wins, manual, auto-merge)
- **Search modes**: Fuzzy, regex, type-aware, graph traversal are strategy variants

### 6. Repository/Unit of Work Pattern

- `persist.SQLiteStore`: Data access layer for call graph persistence
- `memory.EntityStore` and `memory.RelationshipStore`: Repository abstractions
- `DocumentRegistry`: In-memory repository for governance documents

### 7. Builder Pattern

```go
// pkg/callgraph/graph.go
builder := callgraph.NewGraphBuilder()
graph, _ := builder.Build(ctx, "/path/to/module")
```

The `GraphBuilder` accumulates nodes/edges then produces a `Graph`.

### 8. Chain of Responsibility (Audit)

```go
// pkg/safety/audit.go
type AuditLog struct {
    entries []AuditEntry
    chain   []string  // SHA-256 hashes
}

// Each entry hashes the previous, creating tamper-evident chain
func (a *AuditLog) Append(entry AuditEntry) error {
    prevHash := a.chain[len(a.chain)-1]
    entry.PrevHash = prevHash
    entry.Hash = sha256.Sum256(entry.Bytes())
    a.entries = append(a.entries, entry)
    a.chain = append(a.chain, hex.EncodeToString(entry.Hash[:]))
    return nil
}
```

### 9. Anti-Corruption Layer

Two gateway implementations exist:
- `governor/pkg/gateway/` (V2)
- `governor-combined/pkg/gateway/` (consolidated)
- `mcp-gateway/internal/gateway/` (standalone)

The feature flag `ToolDispatchUseRegistry` bridges legacy and new dispatch mechanisms.

### 10. Namespace Convention (MCP Gateway)

```
{server}__{tool_name}
```

Examples:
- `rigour__check_code`
- `adr__list_adrs`
- `repo-butler__staleness_check`

---

## Appendix: Workspace Project Structure

The governor-combined monolith lives within a larger workspace of sibling projects:

```
software_development_lifecycle_management/
├── governor/                    # V2.0 — Code analysis engine (16 packages)
├── governor-combined/           # Monolith — V1+V2 merged (22 packages)
├── hangar/                      # Fleet governance REST client
├── integration/                 # Documentation quality bridge layer
├── mcp-gateway/                 # Central HTTP gateway (aggregates 4 MCP servers)
├── governance-pipelines/        # CI/CD pipeline definitions
├── docs/                        # Specifications & research
├── research/                    # Architecture research
├── data/                        # Runtime data (SQLite, audit logs)
├── ATOMIZED_SPEC.md             # Implementation specifications (4010 lines)
├── research-governance-tool-phase-matrix.md
├── research-project-phase-detection.md
└── research-remediation-breakage-prediction.md
```

### Directory Responsibilities

| Directory | Purpose | Key Types |
|-----------|---------|-----------|
| `governor/` | V2 code analysis engine — MCP server with 7 tools | GraphBuilder, SafetyValidator, Searcher, Memory |
| `governor-combined/` | Monolith consolidation of V1+V2 into single binary | Governor (wiring struct), 15 MCP tools |
| `hangar/` | REST client for fleet-wide policy/scorecard management | Client, Policy, FleetScorecard, RemediationJob |
| `integration/` | Bridges doc quality into Rigour Supervisor + MCP Gateway | DocGate, DocsToolsServer, SupervisorIntegration |
| `mcp-gateway/` | HTTP gateway aggregating 4 stdio MCP servers | Supervisor, Aggregator, StdioClient |
| `governance-pipelines/` | CI/CD workflows with layered config inheritance | GovernanceConfig (CUE-validated) |
| `docs/` | Technical specifications | Phase detection ML spec, doc quality gates |
| `research/` | Architecture research | MCP gateway patterns, SARIF/DD integration |
| `data/` | Runtime state | SQLite DB, safety audit log |

---

## Appendix: Architecture Layers

The system follows a **layered architecture** with clear separation of concerns:

```
+-----------------------------------------------------------+
|                    CI/CD Layer                             |
|              governance-pipelines/                         |
|         (GitHub Actions, CUE schema)                      |
+----------------------------+------------------------------+
                             | triggers workflows
+----------------------------v------------------------------+
|                  Orchestration Layer                       |
|                  mcp-gateway/                              |
|    (HTTP gateway, process supervision, tool aggregation)  |
+----------+---------------+---------------+----------------+
           |               |               |
+----------v----+ +--------v----+ +--------v--------+
|   governor/   | |   hangar/   | | integration/    |
|  (V2 core)    | | (fleet API) | | (doc gate)      |
+---------------+ +-------------+ +-----------------+
           |               |               |
+----------v---------------v---------------v----------------+
|                  Analysis Layer                           |
|    callgraph, safety, search, deadcode, untested,        |
|    memory, docgov, sarif, rigour, collab, ...            |
+-----------------------------------------------------------+
```

### Layer Responsibilities

1. **CI/CD Layer** (`governance-pipelines/`): Defines governance rules with layered inheritance (global -> org -> repo). Orchestrates all tools as GitHub Actions workflows.

2. **Orchestration Layer** (`mcp-gateway/`): Connects to multiple stdio-only MCP servers and exposes them via HTTP. Aggregates tools from Rigour (code quality), AdrMcp (architecture decisions), and Repo Butler (portfolio health) into a unified HTTP API.

3. **Domain Layer** (`governor/`, `hangar/`, `integration/`): Contains the core business logic. Each module is self-contained with its own persistence, domain types, and interfaces.

4. **Analysis Layer** (`governor/pkg/*`): Leaf packages implementing specific analysis capabilities. Each package is self-contained with no cross-package imports.

---

## Appendix: Related Workspace Components

Governor-Combined operates alongside several sibling projects in the workspace. Each is described below for context.

### MCP Gateway — Central Orchestration

The `mcp-gateway/` is the **backbone** of the entire system — it aggregates tools from multiple stdio-only MCP servers and exposes them via HTTP.

#### Architecture (4 layers)

```
cmd/mcp-gateway/main.go          -- Entrypoint
internal/supervisor/supervisor.go -- Process lifecycle (spawn, health check, restart)
internal/stdio/client.go          -- JSON-RPC 2.0 over stdin/stdout
internal/gateway/aggregator.go    -- Tool namespacing, scoped views, routing
internal/gateway/handlers.go      -- HTTP handlers (JSON-RPC proxy, tools/list, tools/call)
```

#### Internal Packages

| Package | Purpose |
|---------|---------|
| `adr/` | ADR pipeline, scheduling, webhooks, connection pool |
| `sarif/` | SARIF 2.1.0 types, tool-specific mappers |
| `rigour/` | Full Rigour supervisor: state machine, fix packets, DLP, brain |
| `repo-butler/` | Portfolio health with 12 tools + 3 resources |
| `defectdojo/` | DefectDojo API v2 client with Jira sync |
| `dedup/` | Cross-tool deduplication engine |
| `metrics/` | Prometheus metrics (server health, tool calls, HTTP) |
| `mcpproto/` | JSON-RPC 2.0 wire types |

#### Key Patterns

- **Namespace convention**: `{server}__{tool}` (e.g., `rigour__check_code`)
- **Scoped tool views**: `full`, `code-review`, `architecture`, `health` with glob-based include/exclude
- **Process supervision**: SIGTERM -> SIGKILL escalation, exponential backoff, crash detection
- **Stderr ring buffer**: 500-line non-blocking capture to prevent pipe backpressure
- **Notification dispatch**: Separate handling for responses (with ID) vs notifications (without ID)

#### Supervised MCP Servers

| Server | Description |
|--------|-------------|
| Rigour | Code quality enforcement, fix packets, DLP, brain |
| AdrMcp | Architecture Decision Records management |
| Repo Butler | Portfolio health, staleness, council findings |
| Hangar | Fleet compliance, policies, scorecards |

### Hangar — Fleet Governance Client

`hangar/` is a production-grade Go client for interacting with "Hangar," a fleet-wide repository governance server managing 1000+ repositories.

#### Domain Model

```
Policy ---+-- PolicyRule
          +-- PolicyTarget ---> RepoRef

FleetScorecard ---+-- RepoScorecard
                  +-- ConnectionScore

RemediationJob ---+-- RemediationAction ---> RepoRef
                  +-- RemediationResult

AuditEvent ---> cursor-based pagination
```

#### Transport Stack

```
Request
  |
  v
LoggingMiddleware
  |
  v
RetryMiddleware (exponential backoff + jitter)
  |
  v
CircuitBreaker (closed/open/half-open)
  |
  v
http.Transport
```

#### Key Patterns

- **Optimistic locking** via ETag/If-Match for concurrent policy sync (409 Conflict handling)
- **Idempotency keys** auto-generated for all mutating operations
- **Parallel fleet fetching** grouped by connection ID (bounded concurrency)
- **Cursor-based pagination** for large fleet queries

### Integration — Documentation Quality Bridge

`integration/` bridges documentation quality evaluation with the Rigour Supervisor and MCP Gateway.

#### Components

| Component | Purpose |
|-----------|---------|
| `DocGate` | Evaluates documentation quality on changed files, returns violations as fix packet instructions |
| `DocsToolsServer` | MCP-compatible tools server exposing 5 tools: `docs_quality_check`, `docs_search`, `docs_reindex`, `docs_stats`, `docs_recommend` |
| `SupervisorIntegration` | Registers doc patterns with Rigour's brain, enriches fix packets with doc violations |
| `DocsIndexProxy` | Reverse proxy to documentation index daemon (port 8082) |
| `AuthMiddleware` | Admin API key authentication with public path allowlisting |

#### Quality Gates

10 documentation quality gates:
1. README completeness
2. CONTRIBUTING guide
3. LICENSE file
4. CHANGELOG maintenance
5. docs/ directory structure
6. API documentation
7. Architecture documentation
8. Runbook documentation
9. Security documentation
10. Compliance documentation

#### Score-to-Severity Mapping

| Score | Severity |
|-------|----------|
| < 30 | Critical |
| < 50 | Error |
| < threshold (70) | Warning |
| >= threshold | Info |

### Governance Pipelines — CI/CD

`governance-pipelines/` defines governance rules for CI/CD pipelines with layered inheritance.

#### Configuration Inheritance

```
global (governance.yaml)
  |
  v
org-level (GitHub org settings)
  |
  v
repo-level (repo-specific overrides)
```

#### Workflows

| Workflow | Purpose |
|----------|---------|
| `hangar-sync.yml` | Fleet sync with Hangar |
| `rigour-check.yml` | Code quality checks via Rigour |
| `adr-validate.yml` | Architecture decision record validation |
| `rb-monitor.yml` | Repository portfolio monitoring |
| `deploy-gate.yml` | Aggregates all check runs, applies severity-based gate decision |
| `_helpers.yml` | Shared workflow helpers |

#### Deploy Gate Logic

1. Collects check runs from all 4 tools
2. Severity levels: success=0, skipped=0, missing=2, pending=3, failure=4
3. Gate passes if worst severity <= 1 (neutral)
4. Missing checks are warnings, not failures (path-filter skip)
5. Manual approval available for production environments

#### CUE Validation

```bash
cue vet -d GovernanceConfig governance.yaml
```

---

## Appendix: Additional Extensibility Patterns

The following extension patterns complement the patterns described in the main Extensibility section.

### Adding a New MCP Server (Gateway)

1. Add server config to `config.yaml`:
```yaml
servers:
  my-tool:
    command: ["./my-tool"]
    args: ["--stdio"]
    namespace: my-tool
```

2. (Optional) Add scoped view:
```yaml
scopes:
  my-scope:
    include: ["my-tool__*"]
```

### Adding a New Governance Pipeline

1. Create workflow in `.github/workflows/`:
```yaml
name: my-check
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: ./governance-pipelines/.github/workflows/_helpers.yml
      - run: my-tool check
```

2. Add tool config to `governance.yaml`:
```yaml
global:
  tools:
    my-tool:
      enabled: true
```

---

## Appendix: Testing Strategy

### Test Patterns

- **Table-driven tests**: Comprehensive coverage with subtests (20+ test cases per feature)
- **Race detector clean**: All packages pass `-race`
- **Integration tests**: End-to-end flows across packages
- **Mock-free**: Most tests use real implementations (stdlib only)

### Test Structure

```go
func TestValidator(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        rules    []string
        want     int // expected finding count
        wantErr  bool
    }{
        {"no issues", "func main() {}", nil, 0, false},
        {"sql injection", `db.Query("SELECT * FROM users WHERE id=" + id)`, nil, 1, false},
        // ... 20+ cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

### Coverage Targets

| Package | Target |
|---------|--------|
| safety | 90%+ (critical path) |
| callgraph | 85%+ |
| search | 80%+ |
| collab | 85%+ |
| mcp | 80%+ |

---

## Appendix: Performance Considerations

| Operation | Target | Notes |
|-----------|--------|-------|
| API diff | < 10s | Syntactic + binary + semantic |
| Test impact analysis | 1-5s | NameRTS: 99.6% safety |
| SAST scan | 5-30s | Per-file pattern matching |
| CodeQL analysis | 5-30min | Weekly scheduled |
| Symbolic execution | minutes-hours | Deep analysis only |
| Call graph build | 10-60s | Depends on codebase size |
| Memory search | < 100ms | Hybrid OMEGA (vector+FTS+graph) |

---

## Appendix: Security Considerations

### LLM-Generated Code

**Critical finding**: LLM-generated fixes introduce **9x more vulnerabilities** than human developers. The system mitigates this through:
- DLP pre-filter (secrets, PII, entropy check)
- Safety validation before applying fixes
- Audit logging of all modifications
- Policy enforcement (warn/block/allow)

### Secret Detection

7 secret patterns detected:
1. AWS access keys
2. GitHub tokens
3. Private keys
4. Connection strings
5. API keys
6. JWT tokens
7. High-entropy strings

### PII Detection

4 PII patterns detected:
1. Email addresses
2. Phone numbers
3. SSN patterns
4. Credit card numbers

---

## Appendix: Package Dependency Graph

```
cmd/governor/main.go
  ├── config/
  ├── pkg/gateway/
  │   └── pkg/mcp/
  ├── pkg/tools/
  │   ├── pkg/callgraph/
  │   │   └── pkg/callgraph/treesitter/
  │   ├── pkg/safety/
  │   ├── pkg/search/
  │   │   └── pkg/callgraph/
  │   ├── pkg/untested/
  │   │   └── pkg/callgraph/
  │   ├── pkg/deadcode/
  │   │   └── pkg/callgraph/
  │   ├── pkg/persist/
  │   └── pkg/mcp/
  ├── pkg/docgov/
  │   └── pkg/staleness/
  ├── pkg/watcher/
  ├── pkg/memory/
  ├── pkg/webhook/
  └── pkg/httpproxy/
```
