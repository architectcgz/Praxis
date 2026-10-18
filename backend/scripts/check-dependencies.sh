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

check_app_contracts() {
  local imports standard package dependencies dependency
  imports="$("$praxis_go" list -f '{{.ImportPath}} {{join .Imports " "}}' ./internal/app/...)"
  standard="$("$praxis_go" list std)"
  while read -r package dependencies; do
    [[ "$package" == praxis/internal/app/*/contracts ]] || continue
    for dependency in $dependencies; do
      # 契约只能组合公开数据，不得借助实现包间接暴露内核或业务对象。
      if [[ "$dependency" == praxis/internal/app/*/contracts ]]; then continue; fi
      if [[ $'\n'"$standard"$'\n' == *$'\n'"$dependency"$'\n'* ]]; then continue; fi
      echo "应用契约依赖了非标准库或实现包: $package -> $dependency" >&2
      return 1
    done
  done <<<"$imports"
}

check_gate './wails/...' 'praxis/internal/(service|core|execution|loop|timing|repository|infra)(/|[[:space:]]|$)'
check_gate './internal/core/model/...' 'praxis/internal/tools/contracts(/|[[:space:]]|$)'
check_gate './internal/service/...' 'praxis/internal/(app|request|infra|execution|loop|compose)(/|[[:space:]]|$)|praxis/wails(/|[[:space:]]|$)|(^|[[:space:]])os(/|[[:space:]]|$)'
check_gate './internal/loop/...' 'praxis/internal/(app|execution|service)(/|[[:space:]]|$)'
check_gate './internal/execution/...' 'praxis/internal/(app|service)(/|[[:space:]]|$)'
check_gate './internal/core/context/...' 'praxis/internal/(core/(task|session|agent)|repository|service|infra|execution|loop)(/|[[:space:]]|$)'
check_gate './internal/infra/jsonl/...' 'praxis/internal/hooks(/|[[:space:]]|$)'
check_gate './internal/app/...' 'praxis/internal/(infra|execution|loop|repository|compose)(/|[[:space:]]|$)|praxis/wails(/|[[:space:]]|$)|(^|[[:space:]])os(/|[[:space:]]|$)'
check_gate './internal/core/...' 'praxis/internal/(app|service|request|repository|infra|execution|loop|compose|hooks|tools)(/|[[:space:]]|$)|praxis/wails(/|[[:space:]]|$)'
check_gate './internal/request/...' 'praxis/internal/(service|repository|infra|execution|loop|compose)(/|[[:space:]]|$)|praxis/internal/app/[^/[:space:]]+([[:space:]]|$)|praxis/wails(/|[[:space:]]|$)'
check_gate './...' 'praxis/internal/contracts(/|[[:space:]]|$)'
check_app_contracts
echo '全部依赖门禁通过。'
