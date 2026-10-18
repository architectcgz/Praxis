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
  if (pkg ~ /^praxis\/internal\/core\// && dep ~ /^praxis\/(internal\/(service|infra|bootstrap)|wails)(\/|$)/) reject("core dependency")
  if (pkg ~ /^praxis\/internal\/core\// && dep == "os") reject("core filesystem I/O")
  if (pkg ~ /^praxis\/internal\/service\// && (dep == "os" || dep ~ /^praxis\/internal\/(infra|bootstrap)(\/|$)/ || dep ~ /^praxis\/wails(\/|$)/)) reject("service dependency")
  if (pkg ~ /^praxis\/internal\/infra\// && dep ~ /^praxis\/internal\/service\/[^/]+\//) reject("infra business orchestration")
  if (pkg ~ /^praxis\/internal\/infra\/jsonl(\/|$)/ && dep ~ /^praxis\/internal\/core\/hook(\/|$)/) reject("repository business hook")
  if (dep ~ /^praxis\/internal\/(execution|runtime|loop|model|tool|task|message|context|hook|storage)(\/|$)/) reject("obsolete module")
 }
}
END {exit failed}
' <<< "$imports"
