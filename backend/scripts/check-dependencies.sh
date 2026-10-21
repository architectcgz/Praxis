#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
imports=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...)
awk '
function reject(reason) { print pkg ": " dep " (" reason ")" > "/dev/stderr"; failed=1 }
{
 pkg=$1
 for (i=2;i<=NF;i++) {
  dep=$i
  if (pkg ~ /^praxis\/internal\/core\// && dep ~ /^praxis\/(internal\/(repository|service|infra|bootstrap|loop|timing|workspace)|wails)(\/|$)/) reject("core dependency")
  if (pkg ~ /^praxis\/internal\/core\// && dep == "os") reject("core filesystem I/O")
  if (pkg ~ /^praxis\/internal\/repository(\/|$)/ && dep ~ /^praxis\// && dep !~ /^praxis\/internal\/(core|identity|clock|fault)(\/|$)/) reject("repository dependency")
  if (pkg ~ /^praxis\/internal\/repository(\/|$)/ && (dep == "os" || dep ~ /^net(\/|$)/)) reject("repository external I/O")
  if (pkg ~ /^praxis\/internal\/service\// && (dep == "os" || dep ~ /^praxis\/internal\/(infra|bootstrap)(\/|$)/ || dep ~ /^praxis\/wails(\/|$)/)) reject("service dependency")
  if (pkg ~ /^praxis\/internal\/loop(\/|$)/ && (dep == "os" || dep ~ /^praxis\/(internal\/(service|infra|bootstrap)|wails)(\/|$)/)) reject("loop dependency")
  if (pkg ~ /^praxis\/internal\/infra\// && dep ~ /^praxis\/(internal\/(service|bootstrap|loop)|wails)(\/|$)/) reject("infra business orchestration")
  if (pkg ~ /^praxis\/wails(\/|$)/ && dep ~ /^praxis\/internal\/(repository|infra|loop)(\/|$)/) reject("wails persistence dependency")
  if (pkg ~ /^praxis\/internal\/infra\/jsonl(\/|$)/ && dep ~ /^praxis\// && dep != "praxis/internal/utils/pathutil") reject("JSONL persistence dependency")
  if (pkg ~ /^praxis\/internal\/infra\/storage(\/|$)/ && dep ~ /^praxis\/internal\/core\/hook(\/|$)/) reject("repository business hook")
  if (dep ~ /^praxis\/internal\/(execution|runtime|model|tool|task|message|context|hook|storage|transaction|security)(\/|$)/ || dep ~ /^praxis\/internal\/(core\/loop|service\/agent\/input)(\/|$)/) reject("obsolete module")
 }
}
END {exit failed}
' <<< "$imports"

# 持久化只允许单事件写入，静态拒绝通用事务和批量提交入口。
if obsolete=$(rg -n '\b(InTx|transactionKey|prepareCommit|validateRestoredTransaction)\b|^type (transaction|Commit)\b|jsonl\.Commit\b' internal/infra/storage internal/infra/jsonl internal/repository --glob '*.go'); then
 printf '%s\n' "$obsolete" >&2
 printf '%s\n' 'persistence must append one event without a transaction workspace' >&2
 exit 1
else
 status=$?
 if (( status != 1 )); then
  exit "$status"
 fi
fi

# 仓储契约集中在 Repository，禁止在消费方重新声明持久化接口。
if contracts=$(rg -n '^type (Store|[[:alnum:]]*Store|Reader|Writer|Streams|WriterProvider|ToolExecutionReader|MessageReader|MessageWriter|MessageStreams|ExecutionReader|QueueReader|ActiveTaskCounter) interface' internal/core internal/service internal/loop internal/workspace internal/timing); then
 printf '%s\n' "$contracts" >&2
 printf '%s\n' 'persistence interfaces must be declared in internal/repository' >&2
 exit 1
else
 status=$?
 if (( status != 1 )); then
  exit "$status"
 fi
fi
