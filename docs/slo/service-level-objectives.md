# Service Level Objectives

## Availability

- `initialize` should succeed for a valid repository root.
- `tools/list` should return the server tool inventory.

## Functional Objectives

- `validate_code` should return a result for a readable file.
- `audit_project` should return analysis instead of `[GRAPH_EMPTY]`.

## Operational Objectives

- Empty cached graphs should be rebuilt.
- Smoke tests should pass before release.
