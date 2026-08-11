#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
server_name="${SERVER_NAME:-governor-combined}"
binary_name="${BINARY_NAME:-governor}"
codex_bin="${CODEX_BIN:-codex}"
runner="$repo_root/scripts/run-governor-stdio.sh"

if ! command -v "$codex_bin" >/dev/null 2>&1; then
  echo "codex is not installed or not on PATH: $codex_bin" >&2
  exit 1
fi

cd "$repo_root"

go build -o "$repo_root/$binary_name" ./cmd/governor

if "$codex_bin" mcp list 2>/dev/null | awk -v name="$server_name" '$1 == name {found=1} END {exit(found ? 0 : 1)}'; then
  "$codex_bin" mcp remove "$server_name" >/dev/null 2>&1 || true
fi

"$codex_bin" mcp add "$server_name" -- "$runner"

echo "Registered $server_name via codex mcp using $runner"
