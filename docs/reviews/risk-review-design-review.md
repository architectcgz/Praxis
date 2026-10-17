# 风险审核职责与重复设计审查

## 结论

当前同时存在两个需要区分的现象：风险审核只有一个 Provider 调用实现；工具调用的领域状态与持久化则有两套并存实现，属于重复设计。

`internal/tool_execution.Executor` 与 `core/tool_invocation.ToolInvocation` 分别承担执行编排与领域状态，职责可以并存。重复的是 `core/tool_execution.State / ApplyRecord` 与 `core/tool_invocation.ToolInvocation / ValidateUpdate`：两者都负责同一次调用的身份、冻结参数、审批、启动、终态和副作用约束，并各自配有仓储。

当前执行主链使用 `ToolInvocationRepository`；事件模型的 `ToolExecutionRepository` 已接入存储层，但没有业务执行器消费者。建议优先收敛为主链已使用的一套领域模型与仓储；另有初次授权与审核准备重复编排的问题。

## 范围与依据

- 基线：`f78a7b8`，源码工作区干净；本审查报告为新增文件。
- 范围：`internal/tool_execution`、`core/tool_execution`、`core/tool_invocation`、`core/security`、两套工具仓储，以及组合根、Loop、JSONL 校验与索引、人工审批和恢复调用方。
- 方法：按 ponytail-review 检查复杂度，并参考 `code-review@claude-plugins-official` 的证据核对要求。该插件入口位于 `C:/Users/Administrator/.claude/plugins/marketplaces/claude-plugins-official/plugins/code-review/commands/code-review.md`；本次对象是本地设计，没有执行其 PR 检查和评论流程。
- 本次仅阅读源码并记录审查结果，未修改业务代码，未运行构建、测试或开发服务；不能据此断言某次实际运行的请求次数。

## 职责划分

| 位置 | 实际职责 | 是否请求 Provider |
| --- | --- | --- |
| [`internal/tool_execution/review.go`](../../backend/internal/tool_execution/review.go) | 开始审核事务、预留请求序号、控制超时、提交结果 | 通过注入函数调用模型审核 |
| [`internal/tool_execution/risk_review.go`](../../backend/internal/tool_execution/risk_review.go) | 组装审核 Prompt 和待审数据、调用 Provider、解析结论 | 是，`RiskReviewer.Review` 第 84 行 |
| [`core/tool_invocation/risk_review.go`](../../backend/internal/core/tool_invocation/risk_review.go) | 定义审核记录，将结论转换为批准、拒绝或人工等待状态 | 否 |
| [`core/security/risk_assessment.go`](../../backend/internal/core/security/risk_assessment.go) | 定义并校验 `safe / dangerous / uncertain` 结论 | 否 |
| [`compose/application.go`](../../backend/internal/compose/application.go) | 将 `reviewer.Review` 注入 Executor | 否 |

`RiskReviewer` 是执行模型审核的组件，`RiskReview` 是一次审核的持久化记录，`RiskAssessment` 是结论值；三者没有相同的执行职责。

## 中：同一次工具调用有两套领域状态与持久化实现

**位置**：`backend/internal/core/tool_execution/records.go:195`、`backend/internal/core/tool_execution/state.go:13`、`backend/internal/core/tool_invocation/tool_invocation.go:109`、`backend/internal/core/tool_invocation/update.go:32`，以及 `repository/tool_execution.go` 和 `repository/tool_invocation.go`。

**问题描述**：这两套 Core 模型没有表达“调用声明与独立执行尝试”这样的父子关系，而是各自覆盖同一个调用的完整执行状态：

| 业务约束 | `core/tool_invocation` | `core/tool_execution` |
| --- | --- | --- |
| 身份归属 | `ToolInvocation` 的 Task、Agent、Session、CallID | `CallKey` 的 Task、Agent、Session、CallID |
| 冻结参数 | `NormalizedArguments`、`Path` | `Preparation` |
| 工具额度预留 | `BudgetReserved` | `Reserved` |
| 审批 | `Approval`、授权领域方法 | `Approval`、审批记录重放 |
| 启动与终态 | `Start / Succeed / Fail / MarkResultUnknown` | `ApplyRecord` 重建启动及 `Outcome` |
| 参数摘要 | `ArgumentsDigest` | 相同实现的 `ArgumentsDigest` |

当前组合根在 `application.go:299` 将 `repos.ToolInvocations` 注入 Executor，Loop、人工审批和业务恢复也使用调用实体。`repos.ToolExecutions` 只在仓储集合中暴露，没有被这些业务组件消费。事件模型的消息字段、索引与事务校验已经接入 JSONL，因此不能把它视为仅删除目录就能清理的孤立代码。

**影响范围／风险**：审批、参数绑定、额度、终态和恢复规则需要维护两个版本，存储层还承担两种模型的互斥和分支校验。例如 `tool_state.go:86`、`validation.go:372`、`tool_receipts.go:53` 均需要区分调用实体与执行记录。状态语义也已不同：实体模型有 `running` 和 `reviewing`，事件模型在提交 Started 后直接重建为 `unknown`，且没有风险审核记录。未发现当前主链同时启动两套执行器；现有互斥校验限制同一 Task 混用模型。

**修正建议**：保留主链使用的 `core/tool_invocation` 作为唯一调用状态来源，保留 `internal/tool_execution.Executor` 处理执行编排。删除没有业务消费者的事件状态模型、对应仓储及 JSONL 中配套的分支、字段与索引，统一现有回执与恢复约束；删除前核对实际持久化数据，不静默丢弃无法恢复的记录。仅改名或合并目录不能消除两套状态规则。

**可选方案**：若产品明确要求通过执行事件重建状态，则让 Executor、Loop、人工审批、额度和恢复统一使用事件模型，并完整承载风险审核约束，再移除独立调用实体。该方案涉及完整主链切换，当前并无两套模型长期并存的业务依据。

## 中：初次授权与审核准备重复编排

**位置**：`backend/internal/tool_execution/authorization.go:44`、`backend/internal/tool_execution/review.go:24`。

**问题描述**：正常的 prepared → auto 路径中，`authorize` 先开启事务，读取调用、执行 `rules.Decide`、应用 `ApplyAuthorization` 并保存。提交后，`review` 再开启事务，读取同一调用、再次执行 `rules.Decide` 和 `ApplyAuthorization`，随后创建审核身份、预留请求序号并保存。

**影响范围／风险**：每次初始 auto 审核增加一次准备事务及重复的规则解析、授权应用和保存路径，并使规则变化的处理分散在两个入口。本项是复杂度问题，不是重复请求 Provider 的证据，也不代表当前重新读取规则的行为本身错误；在事务尚未合并时，不能直接删除第二次规则读取。

**修正建议**：在统一授权事务中完成动作解析；动作是 auto 时，在同一事务内创建审核身份、预留请求序号并保存。事务提交后调用模型，再以独立事务复核当前规则并提交审核结论。工具启动前仍保留现有授权复核。恢复未完成审核时复用同一准备逻辑，保持每次尝试的独立身份与历史记录。

**可选方案**：保留现有结构，只明确命名和职责。这可以降低导航成本，但不会减少重复编排；不建议仅为减少文件数量而把 Core 状态转换并入 Provider 调用组件。

## 必须保留的边界与验证方式

- Provider 请求位于事务之外，不能在等待网络响应时持有存储事务锁。
- 模型返回后的规则复核与执行前复核覆盖不同并发窗口，不能按重复审核删除。
- 冻结参数摘要、审核身份、已完成记录不可变、同一调用的审核并发保护，以及超时和非法输出转人工的行为均应保留。
- 收敛状态模型时，确认 Executor、Loop、人工审批和启动恢复均使用同一仓储；冻结参数、风险结论、工具额度、回执配对和未知副作用禁止重放均有唯一规则来源。
- 实施后按项目要求执行 `go build ./...`、`go vet ./...` 和 `scripts/check-dependencies.sh`；人工回归覆盖 safe、dangerous、uncertain、超时、审核期间规则变化和审核中断后恢复。
- 验证一次新 auto 调用正常完成时只产生一次风险审核 Provider 请求；规则变化或中断恢复触发的新尝试单独核对。

net: 未实施简化，代码净减少行数未测量。
