#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
praxis_go="${PRAXIS_GO:-go}"
check_gate() {
  local packages="$1" pattern="$2" imports result status
  imports="$("$praxis_go" list -f '{{.ImportPath}} {{join .Imports " "}}' "$packages")"
  if result="$(rg "$pattern" <<<"$imports")"; then
    echo "禁止的依赖 ($packages):" >&2
    echo "$result" >&2
    return 1
  else
    status=$?
    if [ "$status" -ne 1 ]; then return "$status"; fi
  fi
}
check_gate './wails/...' 'praxis/internal/(core|execution|loop|timing|repository|infra)(/|[[:space:]]|$)'
check_gate './internal/core/model/...' 'praxis/internal/tools/contracts(/|[[:space:]]|$)'
check_gate './internal/service/...' 'praxis/internal/execution(/|[[:space:]]|$)|(^|[[:space:]])os([[:space:]]|$)'
check_gate './internal/loop/...' 'praxis/internal/(execution|service)(/|[[:space:]]|$)'
check_gate './internal/execution/...' 'praxis/internal/service(/|[[:space:]]|$)'
check_gate './internal/core/context/...' 'praxis/internal/(core/(task|session|agent)|repository|service|infra|execution|loop)(/|[[:space:]]|$)'
check_gate './internal/infra/jsonl/...' 'praxis/internal/hooks(/|[[:space:]]|$)'
echo 'All 7 dependency gates passed.'
