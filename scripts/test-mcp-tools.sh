#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${1:-$repo_root/governor}"

if [[ ! -x "$binary" ]]; then
  echo "Governor binary not found or not executable: $binary" >&2
  exit 1
fi

response="$(
  {
    printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
    printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  } | "$binary" --stdio --config "$repo_root/governor.yaml" 2>/dev/null
)"

init_response="$(printf '%s\n' "$response" | sed -n '1p')"
tools_response="$(printf '%s\n' "$response" | sed -n '2p')"

if [[ -z "$init_response" || -z "$tools_response" ]]; then
  echo "Expected initialize and tools/list responses, got:" >&2
  printf '%s\n' "$response" >&2
  exit 1
fi

echo "$init_response" | grep -q '"name":"governor"' || {
  echo "Unexpected initialize response:" >&2
  echo "$init_response" >&2
  exit 1
}

echo "$tools_response" | grep -q '"tools"' || {
  echo "tools/list response missing tools payload:" >&2
  echo "$tools_response" >&2
  exit 1
}

printf '%s\n%s\n' "$init_response" "$tools_response"
