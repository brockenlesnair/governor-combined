# MCP Tools Reference

Governor exposes 15 tools via the MCP protocol. All tools accept JSON arguments and return JSON results.

## V2 Core Tools (7)

### `validate_code`

Validate a source file for dangerous patterns, policy violations, and security issues.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `file` | string | yes | - | Path to the file to validate |
| `content` | string | no | - | Optional file content |
| `policy` | string | no | `"default"` | Policy name |

**Response:**
```json
{
  "file": "main.go",
  "policy": "default",
  "passed": true,
  "risk_score": 2.5,
  "risk_level": "medium",
  "findings": [...],
  "blocked": false,
  "reason": "",
  "checked_files": 1,
  "duration": "1.2ms"
}
```

### `validate_diff`

Validate only changed lines in a unified diff.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `diff` | string | yes | - | Unified diff content |
| `policy` | string | no | `"default"` | Policy name |

### `search_code`

Search for code symbols using fuzzy, regex, or type-aware queries.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `query` | string | yes | - | Search query |
| `type` | string | no | `"fuzzy"` | Search type: `fuzzy`, `regex`, `callers`, `callees`, `type`, `exact`, `prefix`, `substring`, `kind` |
| `max_results` | int | no | `50` | Maximum results |
| `target_id` | string | no | - | Target function for caller/callee searches |
| `depth` | int | no | `3` | Traversal depth |
| `param_types` | string[] | no | - | Parameter types for type-aware search |
| `return_types` | string[] | no | - | Return types for type-aware search |

### `get_callers`

Get all callers of a function (direct or transitive).

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `function_id` | string | yes | - | Function ID |
| `transitive` | bool | no | `false` | Return transitive callers |
| `depth` | int | no | `3` | Max traversal depth |

### `get_callees`

Get all callees of a function (direct or transitive).

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `function_id` | string | yes | - | Function ID |
| `transitive` | bool | no | `false` | Return transitive callees |
| `depth` | int | no | `3` | Max traversal depth |

### `get_impact`

Analyze the impact of changing a function.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `function_id` | string | yes | - | Function ID |
| `depth` | int | no | `3` | Max traversal depth |

**Response:**
```json
{
  "function_id": "Handler",
  "depth": 3,
  "direct_callers": ["main", "Router.Handle"],
  "transitive_callers": ["main", "Router.Handle", "Server.Listen"],
  "affected_functions": ["main", "Router.Handle"],
  "risk_level": "medium",
  "description": "Changing Handler affects 2 functions directly and 3 transitively"
}
```

### `audit_project`

Run a full project audit: untested code, dead code, and safety issues.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `include_untested` | bool | no | `true` | Include untested code detection |
| `include_deadcode` | bool | no | `true` | Include dead code detection |
| `include_safety` | bool | no | `true` | Include safety validation |
| `min_priority` | float | no | `0.3` | Min priority for untested functions |
| `min_confidence` | float | no | `0.5` | Min confidence for dead code |

## V1 Extended Tools (8)

### `rigour_check`

Run DLP pre-filter scan on code text before agent processing.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `text` | string | yes | - | Code text to scan |
| `file_path` | string | no | - | File path for context |

**Response:**
```json
{
  "blocked": true,
  "reasons": ["secret pattern detected", "PII detected: email"],
  "entropy_hits": [...],
  "secret_hits": [...],
  "pii_hits": [...],
  "false_positive_exemptions": 0,
  "file_path": "config.go"
}
```

### `rigour_state`

Get current rigour supervisor state.

**Response:**
```json
{"state": "idle"}
```

States: `idle`, `working`, `fixing`, `handoff`, `done`

### `rigour_stats`

Get rigour brain statistics.

**Response:**
```json
{
  "pattern_count": 42,
  "hard_rule_count": 5,
  "avg_strength": 0.75,
  "max_strength": 0.98
}
```

### `sarif_export`

Export findings as a SARIF 2.1.0 report.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `findings` | array | yes | - | Findings to export |
| `source` | string | no | `"governor"` | Source label |

**Response:**
```json
{
  "report": "{...SARIF JSON...}",
  "findings": 1,
  "source": "safety"
}
```

### `adr_create`

Create an Architecture Decision Record.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `title` | string | yes | - | ADR title |
| `status` | string | no | `"proposed"` | Status: `proposed`, `accepted`, `deprecated`, `superseded` |
| `diff` | string | no | - | Diff or context |

### `adr_list`

List existing Architecture Decision Records.

No arguments required.

### `hangar_score`

Get the fleet compliance scorecard from Hangar.

**Arguments:**
| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `page` | int | no | `0` | Page number (0 = all) |
| `page_size` | int | no | `100` | Results per page |

### `repo_health`

Get the repo health tier from Repo Butler.

No arguments required.
