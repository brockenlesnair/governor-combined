# ADR Guide

Architecture Decision Records (ADRs) capture important architectural decisions along with their context and consequences. Governor provides an ADR system with GitHub integration, PR-based workflows, and slash command automation.

## Concepts

### ADR Document

An ADR has:
- **Title**: Decision name
- **Status**: `proposed`, `accepted`, `deprecated`, `superseded`
- **Content**: Full decision text
- **Path**: File location in `adr_root`

### GitHub Integration

Governor integrates with GitHub via:
- **Webhooks**: Receives PR events to trigger ADR proposals
- **PR Comments**: Posts preview diffs and accepts slash commands
- **Slash Commands**: `/adr accept`, `/adr reject`, `/adr update`

### Workflow

```
PR Created → Diff Analysis → ADR Proposal → PR Comment
                                                  ↓
                                          /adr accept → Create ADR
                                          /adr reject → Reject proposal
                                          /adr update → Update preview
```

## Configuration

```yaml
features:
  adr:
    enabled: true
    adr_root: "./docs/adr"
    repo_root: "."
    github:
      webhook_secret: ""
      token: ""
    preview:
      comment_tag: "<!-- governor-adr-preview -->"
```

## Types

### ADRDocument

```go
type ADRDocument struct {
    Title   string `json:"title"`
    Status  string `json:"status"` // proposed, accepted, deprecated, superseded
    Content string `json:"content"`
    Path    string `json:"path"`
}
```

### PRDiff

```go
type PRDiff struct {
    Files       []DiffFile `json:"files"`
    TotalAdd    int        `json:"total_add"`
    TotalDelete int        `json:"total_delete"`
    TotalFiles  int        `json:"total_files"`
}
```

### ConflictPair

Detects conflicting ADRs:
```go
type ConflictPair struct {
    ADR1        string   `json:"adr1"`
    ADR2        string   `json:"adr2"`
    Similarity  float64  `json:"similarity"`
    OverlapPaths []string `json:"overlap_paths"`
}
```

## Slash Commands

### `/adr accept`

Accepts the ADR proposal and creates the record:

```
/adr accept
```

### `/adr reject [reason]`

Rejects the proposal with optional reason:

```
/adr reject Not aligned with current architecture
```

### `/adr update [changes]`

Updates the proposal and shows new diff:

```
/adr update Added section on performance implications
```

## Preview Comments

When an ADR is proposed, Governor posts a preview comment:

```markdown
## ADR Proposal: Use PostgreSQL for Session Storage

**Status:** proposed

### Proposed Changes

```diff
+## Decision
+We will use PostgreSQL for session storage.
+
+## Consequences
+- Requires database migration
+- Better ACID guarantees
```

**Affected files:** `docs/adr/001-postgres-sessions.md`

---

**Accept:** /adr accept
**Reject:** /adr reject [reason]
**Update:** /adr update [changes]
```

## MCP Tools

### `adr_create`

Create an ADR from a template:

```json
{
  "tool": "adr_create",
  "arguments": {
    "title": "Use Redis for Caching",
    "status": "proposed",
    "diff": "+## Decision\nWe will use Redis for caching..."
  }
}
```

### `adr_list`

List existing ADRs:

```json
{"tool": "adr_list"}
```

## Stale ADR Detection

Governor can detect ADRs with stale code references:

```go
type StaleADR struct {
    ADRPath      string    `json:"adr_path"`
    CodeRefs     []CodeRef `json:"code_refs"`
    LastVerified time.Time `json:"last_verified"`
    Reason       string    `json:"reason"`
}
```

## CI Hook

The `CIHook` integrates with CI pipelines to:
- Validate ADR references exist
- Check for conflicting ADRs
- Enforce ADR status transitions
