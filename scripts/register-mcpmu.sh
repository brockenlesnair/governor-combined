#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
server_name="${SERVER_NAME:-governor-combined}"
binary_name="${BINARY_NAME:-governor}"
mcpmu_bin="${MCPMU_BIN:-mcpmu}"

if ! command -v "$mcpmu_bin" >/dev/null 2>&1; then
  echo "mcpmu is not installed or not on PATH: $mcpmu_bin" >&2
  exit 1
fi

cd "$repo_root"

go build -o "$repo_root/$binary_name" ./cmd/governor

if "$mcpmu_bin" list 2>/dev/null | awk -v name="$server_name" '$1 == name {found=1} END {exit(found ? 0 : 1)}'; then
  "$mcpmu_bin" remove "$server_name" --yes >/dev/null 2>&1 || true
fi

"$mcpmu_bin" add "$server_name" --autostart --cwd "$repo_root" -- "$repo_root/$binary_name" --stdio --config "$repo_root/governor.yaml"

echo "Registered $server_name via mcpmu using $repo_root/$binary_name"
