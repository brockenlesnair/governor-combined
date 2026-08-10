# Callgraph Guide

The callgraph system builds a function-level call graph from source code using tree-sitter parsing. It supports impact analysis, dead code detection, cycle detection, and centrality metrics.

## Concepts

### Graph Structure

The graph contains:
- **Nodes**: Functions, methods, types, packages
- **Edges**: Call relationships (direct, interface, channel)

```go
type Node struct {
    ID        string
    Name      string
    Package   string
    File      string
    Line      int
    Kind      NodeKind   // function, method, type, package
    Exported  bool
    Receiver  string     // for methods
    FanIn     int        // number of callers
    FanOut    int        // number of callees
    InCycle   bool       // part of a cycle
    IsLeaf    bool       // no outgoing edges
    IsRoot    bool       // no incoming edges
}
```

### Node Kinds

| Kind | Description |
|------|-------------|
| `function` | Top-level function |
| `method` | Method on a type |
| `type` | Type definition |
| `package` | Package-level entity |

## Building the Graph

```go
builder := callgraph.NewGraphBuilder()
graph, err := builder.Build(ctx, "./")
```

The builder:
1. Parses all packages using tree-sitter
2. Extracts function definitions and call sites
3. Resolves interface method calls to implementations
4. Runs analysis (fan-in/fan-out, cycle detection)

## Impact Analysis

Analyze what happens when a function changes:

```go
analyzer := callgraph.NewAnalyzer()
impact := analyzer.AnalyzeImpact(graph, "github.com/user/pkg.Handler")
```

```go
type ImpactAnalysis struct {
    Target      string   `json:"target"`
    DirectDeps  []string `json:"directDeps"`
    TransDeps   []string `json:"transDeps"`
    Affected    []string `json:"affected"`
    RiskLevel   string   `json:"riskLevel"`    // low, medium, high, critical
    Description string   `json:"description"`
}
```

Risk levels:
- `low`: <10 affected functions
- `medium`: 10-20
- `high`: 20-50
- `critical`: >50

## Caller/Callee Queries

### Direct Callers

```go
callers := graph.Callers("function-id")
```

### Transitive Callers (BFS)

```go
edges := graph.TransitiveCallers("function-id", 3) // depth=3
```

### Direct Callees

```go
callees := graph.Callees("function-id")
```

### Transitive Callees (BFS)

```go
edges := graph.TransitiveCallees("function-id", 3)
```

## Dead Code Detection

```go
analysis := analyzer.AnalyzeDeadCode(graph)
```

```go
type DeadCodeAnalysis struct {
    Unreachable   []string `json:"unreachable"`
    UnusedExports []string `json:"unusedExports"`
    Orphaned      []string `json:"orphaned"`
}
```

- **Unreachable**: Not reachable from any exported function
- **UnusedExports**: Exported but not reachable from entry points
- **Orphaned**: No incoming or outgoing edges

## Cycle Detection

Uses Tarjan's algorithm to find strongly connected components:

```go
cycles := graph.FindCycles()
```

## Topological Sort

```go
order, err := graph.TopologicalSort()
// Returns error if graph has cycles
```

## Centrality Metrics

```go
metrics := analyzer.ComputeCentrality(graph)
```

- **Betweenness**: How often a node lies on shortest paths
- **Closeness**: Average distance to all other nodes
- **Degree**: Fan-in + fan-out

## Package Metrics

```go
pkgMetrics := analyzer.ComputePackageMetrics(graph)
```

Returns per-package: node count, edge count, avg/max fan-in/out, cyclic depth.

## Cross-Package Edges

```go
crossEdges := analyzer.CrossPackageEdges(graph)
```

## MCP Tools

### `get_callers`

```json
{
  "tool": "get_callers",
  "arguments": {
    "function_id": "github.com/user/pkg.Handler",
    "transitive": true,
    "depth": 3
  }
}
```

### `get_callees`

```json
{
  "tool": "get_callees",
  "arguments": {
    "function_id": "github.com/user/pkg.Handler",
    "transitive": true,
    "depth": 3
  }
}
```

### `get_impact`

```json
{
  "tool": "get_impact",
  "arguments": {
    "function_id": "github.com/user/pkg.Handler",
    "depth": 3
  }
}
```

## File Watching

The watcher auto-rebuilds the call graph on file changes:

```yaml
features:
  callgraph:
    enabled: true
```

Default poll interval: 2 seconds.
