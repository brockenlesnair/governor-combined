#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${1:-$repo_root/governor}"
target_file="${TARGET_FILE:-README.md}"

if [[ ! -x "$binary" ]]; then
  echo "Governor binary not found or not executable: $binary" >&2
  exit 1
fi

response="$(
  {
    printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
    printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"validate_code","arguments":{"file":"%s"}}}\n' "$target_file"
  } | "$binary" --stdio --config "$repo_root/governor.yaml" 2>/dev/null
)"

init_response="$(printf '%s\n' "$response" | sed -n '1p')"
call_response="$(printf '%s\n' "$response" | sed -n '2p')"

if [[ -z "$init_response" || -z "$call_response" ]]; then
  echo "Expected initialize and tools/call responses, got:" >&2
  printf '%s\n' "$response" >&2
  exit 1
fi

echo "$init_response" | grep -q '"name":"governor"' || {
  echo "Unexpected initialize response:" >&2
  echo "$init_response" >&2
  exit 1
}

if echo "$call_response" | grep -q '"error"'; then
  echo "$call_response" | grep -q '"code":-32000\|"code":-32602' || {
    echo "Unexpected tools/call error:" >&2
    echo "$call_response" >&2
    exit 1
  }
else
  echo "$call_response" | grep -q '"content"' || {
    echo "tools/call response missing content payload:" >&2
    echo "$call_response" >&2
    exit 1
  }
fi

printf '%s\n%s\n' "$init_response" "$call_response"
