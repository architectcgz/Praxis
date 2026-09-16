# 持久化与命令模型实施计划

## 1. 目标结论

本目录包含以下目标状态的实施计划：

1. SQLite 只保存关系、身份、状态、约束、索引、幂等键和恢复查询字段。
2. Document Store 保存正文、输入快照、复杂列表和不适合关系建模的结构化内容。
3. Agent JSONL 保存 Agent 实际看到和产生的 transcript，以及 execution、tool、artifact receipt。
4. Session 表达业务归属，不作为包含全部 Execution、ToolInvocation、Event 的单一大文档。
5. `document_ref` 的完整值是文档定位键，格式为 `{collection}/{object_id}/sha256-{digest}.json`。
6. Event 是可索引的审计事实，直接保存在 SQLite，不为每条 Event 创建 JSON 文件。
7. 幂等优先使用调用方提供的稳定资源 ID 或命令 ID，并由业务表唯一约束保证。
8. Pause 和 Close 使用明确的外部命令名称；内部持久化为可恢复的 Agent control command。

## 2. 计划列表

| 顺序 | 计划 | 交付结果 |
|---|---|---|
| 1 | [关系与文档存储边界](01-storage-boundary-plan.md) | 建立 `sqlite`、`document`、`storage` 三层持久化边界 |
| 2 | [Event 关系化](02-relational-domain-events-plan.md) | 删除 Event 文档和通用 Payload，改为显式关系字段 |
| 3 | [命令幂等简化](03-command-idempotency-plan.md) | 以业务身份和唯一约束替代通用 CommandReceipt |
| 4 | [Agent 控制命令](04-agent-control-command-plan.md) | 提供 `PauseAgent`、`CloseAgent`，并建立可恢复控制状态 |
| 5 | [Context 组织](05-context-organization-plan.md) | 拆分共享事实、私有 transcript、外部引用和 Execution 快照 |

## 3. 实施顺序

第一项先固定存储 package 边界和事务行为。第二、三项可以在该边界上独立实施。第四项依赖第三项定义的命令身份规则，并复用第二项的关系型 Event。第五项依赖第一项的文档引用边界、第三项的 EntryID 幂等规则，并由 Execution 与 Runtime 共同落地。

每项计划独立提交、独立测试。任一阶段结束时必须满足：

- `go test ./...`
- `go vet ./...`
- `git diff --check`
- SQLite schema 不含通用 `payload` 列
- `internal/infrastructure/sqlite` 不依赖 `document`

