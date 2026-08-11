#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${1:-$repo_root/governor}"

if [[ ! -x "$binary" ]]; then
  echo "Governor binary not found or not executable: $binary" >&2
  exit 1
fi

response="$(
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    | "$binary" --stdio --config "$repo_root/governor.yaml" 2>/dev/null \
    | sed -n '1p'
)"

if [[ -z "$response" ]]; then
  echo "No initialize response received" >&2
  exit 1
fi

echo "$response" | grep -q '"name":"governor"' || {
  echo "Unexpected initialize response:" >&2
  echo "$response" >&2
  exit 1
}

echo "$response"
