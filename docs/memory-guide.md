# Memory Guide

Governor's memory system provides persistent storage with hybrid vector/FTS/graph retrieval. It implements FSRS v6 spaced repetition for decay and supports entity relationships for graph traversal.

## Concepts

### Entities

The fundamental unit of memory:

```go
type Entity struct {
    ID          string            `json:"id"`
    Type        EntityType        `json:"type"`      // decision, lesson, fact, etc.
    Content     string            `json:"content"`
    Metadata    map[string]string `json:"metadata"`
    Project     string            `json:"project"`
    Tags        []string          `json:"tags"`
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
    AccessCount int               `json:"access_count"`
    DecayScore  float64           `json:"decay_score"` // 0.0=forgotten, 1.0=fresh
    Embedding   []float32         `json:"-"`           // 384-dim vector
}
```

### Entity Types

| Type | Description |
|------|-------------|
| `decision` | Architectural or design decisions |
| `lesson` | Lessons learned |
| `fact` | Factual information |
| `preference` | User preferences |
| `person` | People information |
| `project` | Project metadata |
| `bug` | Bug reports |
| `pattern` | Code patterns |
| `context` | Contextual information |

### Relationships

Connect entities with typed edges:

```go
type Relationship struct {
    ID        string    `json:"id"`
    SourceID  string    `json:"source_id"`
    TargetID  string    `json:"target_id"`
    Kind      string    `json:"kind"`       // "depends_on", "decided_by", etc.
    Weight    float64   `json:"weight"`     // 0.0-1.0
    CreatedAt time.Time `json:"created_at"`
}
```

## Hybrid Search

The search engine combines three retrieval methods:

### 1. Vector Search

Cosine similarity on 384-dim embeddings (bge-small-en-v1.5 via ONNX Runtime).

### 2. Full-Text Search (FTS)

BM25-like scoring based on term frequency.

### 3. Graph Traversal (Spreading Activation)

BFS from seed entities through relationships with decay factor (0.8 per hop).

### Fusion

Results are deduplicated by entity ID, keeping the highest score:

```
score = max(vector_score * 0.5, fts_score * 0.3, graph_score * 0.2)
```

Configuration:
```go
Config{
    VectorWeight: 0.5,
    FTSWeight:    0.3,
    GraphWeight:  0.2,
}
```

## Decay (FSRS v6)

The decay engine implements FSRS v6 spaced repetition:

```
retention = exp(-days_since_update / stability)
stability = 1.0 + access_count * 0.5
```

- **Decay interval**: How often to run decay passes (default: 1 hour)
- **Prune threshold**: Decay score below which entities are removed (default: 0.1)
- **Access boost**: Each access increases stability: `boost = 1.0 + log(access_count) * 0.1`

## Configuration

```yaml
features:
  memory:
    enabled: true
    storage_path: "./data/memory.db"
```

Advanced config:
```go
Config{
    DBPath:            "./data/memory.db",
    EmbeddingModelPath: "./models/bge-small-en-v1.5.onnx",
    EmbeddingDim:      384,
    MaxEntities:       100000,
    DecayInterval:     time.Hour,
    PruneThreshold:    0.1,
    FSRS: FSRSConfig{
        DesiredRetention: 0.9,
        MinInterval:      1,
        MaxInterval:      36500,
    },
}
```

## Usage

### Store an Entity

```go
mem.Store(ctx, &Entity{
    ID:      "adr-001",
    Type:    EntityDecision,
    Content: "Use PostgreSQL for session storage",
    Project: "my-app",
    Tags:    []string{"database", "sessions"},
})
```

### Search

```go
results, _ := mem.Search(ctx, &SearchQuery{
    Text:     "database sessions",
    Types:    []EntityType{EntityDecision},
    Projects: []string{"my-app"},
    Limit:    10,
    MaxHops:  2,
})
```

### Link Entities

```go
mem.Link(ctx, &Relationship{
    SourceID: "adr-001",
    TargetID: "bug-042",
    Kind:     "resolved_by",
    Weight:   0.9,
})
```

### Traverse Graph

```go
entities, _ := mem.Traverse(ctx, "adr-001", 3)
```

## Storage

Uses SQLite with WAL mode for concurrent access:

```go
db, _ := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
```
