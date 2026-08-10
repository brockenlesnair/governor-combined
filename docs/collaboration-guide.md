# Collaboration Guide

The collaboration system enables multiple AI agents to work on the same codebase safely. It provides shadow files, file locking, merge strategies, and conflict resolution.

## Concepts

### Shadow Files

Each agent works on a temporary copy (shadow) of the original file:

```
.original.go                    # Original file
.governor/shadows/
  original.go.agent-1.20260810.tmp  # Agent 1's shadow
  original.go.agent-2.20260810.tmp  # Agent 2's shadow
```

### Locking

Files can be locked by agents to prevent conflicts:

```go
type LockInfo struct {
    Path       string    `json:"path"`
    AgentID    AgentID   `json:"agent_id"`
    AcquiredAt time.Time `json:"acquired_at"`
    ExpiresAt  time.Time `json:"expires_at"`
    ShadowPath string    `json:"shadow_path"`
}
```

### Merge Strategies

| Strategy | Description |
|----------|-------------|
| `last_write_wins` | Shadow always wins (default) |
| `auto_merge` | Merge non-conflicting lines, shadow wins conflicts |
| `manual` | Return conflicts for manual resolution |

## Configuration

```go
Config{
    ShadowDir:        ".governor/shadows",
    LockDir:          ".governor/locks",
    LockTimeout:      5 * time.Minute,
    LockRetryDelay:   100 * time.Millisecond,
    LockMaxRetries:   30,
    AutoMerge:        true,
    ConflictStrategy: ConflictLastWriteWins,
}
```

## Workflow

### 1. Create Shadow

```go
sm := collab.NewShadowManager(cfg)
shadow, err := sm.CreateShadow(ctx, "main.go", "agent-1")
```

### 2. Edit Shadow

```go
err := sm.WriteShadow(ctx, "main.go", "agent-1", newContent)
```

### 3. Read Shadow

```go
content, err := sm.ReadShadow(ctx, "main.go", "agent-1")
```

### 4. Merge to Original

```go
merger := collab.NewMerger(cfg)
result, err := merger.Merge(ctx, shadow)
```

### 5. Delete Shadow

```go
err := sm.DeleteShadow(ctx, "main.go", "agent-1")
```

## Conflict Detection

The merger performs line-by-line comparison:

```go
conflicts := merger.DetectConflicts(original, shadowContent)
```

```go
type Conflict struct {
    Path       string `json:"path"`
    Line       int    `json:"line"`
    Original   string `json:"original"`
    Shadow     string `json:"shadow"`
    Resolution string `json:"resolution,omitempty"`
}
```

## Conflict Resolution

### Last-Write-Wins

Shadow always wins:
```go
merged := merger.ResolveLastWriteWins(original, shadow, conflicts)
```

### Auto-Merge

Merge non-conflicting lines, shadow wins on conflicts:
```go
merged := merger.ResolveAutoMerge(original, shadow, conflicts)
```

### Manual

Returns conflicts for external resolution:
```go
result, err := merger.Merge(ctx, shadow)
if err != nil {
    // Handle conflicts
    for _, conflict := range result.Conflicts {
        fmt.Printf("Line %d: %s vs %s\n", conflict.Line, conflict.Original, conflict.Shadow)
    }
}
```

## File Status

Check collaboration status of a file:

```go
type FileStatus struct {
    Path        string  `json:"path"`
    Locked      bool    `json:"locked"`
    LockedBy    AgentID `json:"locked_by,omitempty"`
    HasShadow   bool    `json:"has_shadow"`
    ShadowAgent AgentID `json:"shadow_agent,omitempty"`
}
```

## Error Handling

```go
type CollabError struct {
    Code    string // ErrCodeShadowExists, ErrCodeNotLocked, etc.
    Message string
    Path    string
    AgentID AgentID
    Err     error
}
```

Error codes:
- `ErrCodeShadowExists`: Shadow already exists for this agent
- `ErrCodeNotLocked`: Shadow not found
- `ErrCodeFileNotFound`: File operation failed
- `ErrCodeMergeConflict`: Conflicts require manual resolution
- `ErrCodeInvalidAgent`: Empty agent ID

## Listing Shadows

```go
// All shadows
allShadows := sm.ListShadows("")

// Agent-specific shadows
agentShadows := sm.ListShadows("agent-1")
```

## Testing

The system provides test helpers:

```go
shadowPath := sm.ShadowFilePathForTest("main.go", "agent-1")
```
