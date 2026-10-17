# 后端输入校验归属复查

## 结论与范围

报告覆盖的 **6 类问题均已修复**。输入的规范化、默认值补充和字段校验由请求、配置读取、模型响应解析及持久化读取入口承担；内部使用 canonical 数据，领域方法负责状态转换与业务关系。

核查依据为 [`backend/AGENTS.md`](../../backend/AGENTS.md)。本报告覆盖以下调用链，不代表已穷尽后端全部业务路径。

## 核查结果

| 核查项 | 当前输入边界及证据 | 内部行为 | 状态 |
| --- | --- | --- | --- |
| 实体与消息字段校验 | [`JSONL 恢复分派`](../../backend/internal/infra/jsonl/validation.go)、[`实体恢复校验`](../../backend/internal/infra/jsonl/entities_validation.go)、[`消息输入校验`](../../backend/internal/infra/jsonl/messages.go)、[`Agent 定义加载`](../../backend/internal/infra/agent_registry/config.go) | Project、Workspace、Session、Agent、WaitCondition、ModelUsageRecord、AgentDefinition 和 MessageData 不再持有这些输入校验方法 | 通过 |
| Task 与控制命令 | 请求入口检查输入，JSONL 独立检查恢复的 Task 和控制命令 | [`Task 构造与准备`](../../backend/internal/core/task/task.go)、[`控制命令构造`](../../backend/internal/core/agent/control.go) 及 [`Coordinator 激活`](../../backend/internal/execution/coordinator.go) 使用 canonical 数据，保留状态、占用及幂等约束 | 通过 |
| 模型配置与 URL | [`模型配置请求`](../../backend/internal/request/model_config.go)、[`配置字段校验`](../../backend/internal/request/model_config_validation.go)、[`URL 校验`](../../backend/internal/request/model_url.go)、[`配置文件读取`](../../backend/internal/infra/model_registry/config_store.go)；Task 恢复独立检查冻结模型配置和 URL | [`BuildModel`](../../backend/internal/infra/model_registry/model_builder.go) 使用冻结字段，检查当前凭据、client 和 stream 构建结果；内部不使用 ValidatedConfig 或 prepared 标志 | 通过 |
| 风险审核结论与工具调用状态 | [`模型结果解析`](../../backend/internal/tool_execution/risk_review.go) 检查 verdict 与 reason；[`工具调用恢复`](../../backend/internal/infra/jsonl/tool_validation.go) 检查持久化参数、审核历史和审批绑定 | [`审核编排`](../../backend/internal/tool_execution/review.go) 不复查同一结论；[`状态更新`](../../backend/internal/core/tool_execution/execution.go) 保留业务约束，不回扫不变身份、参数 JSON 和历史 reason | 通过 |
| SessionContextEntry | [`NewAppendSessionContext`](../../backend/internal/request/commands.go) 完成输入规范化和校验；JSONL 恢复只读检查正文、摘要和时间 | [`core 构造器`](../../backend/internal/core/session/context.go) 使用 canonical 正文计算摘要，服务接收 canonical request | 通过 |
| 工具规范化参数 | [`Bash Normalizer`](../../backend/internal/tools/bash/normalizer.go) 处理新调用；[`JSONL 恢复校验`](../../backend/internal/infra/jsonl/tool_validation.go) 独立检查冻结参数，不依赖工具实现包，不填默认值或清洗冻结数据 | [`调用执行器`](../../backend/internal/tool_execution/executor.go) 不再对 Normalizer 结果重复执行 json.Valid；[`Bash 执行器`](../../backend/internal/tools/bash/executor.go) 解析执行参数并检查授权对象及当前访问范围 | 通过 |

补充核查：Bash 策略文件的规则校验由 [`toolconfig.Load`](../../backend/internal/infra/toolconfig/config.go) 承担；最终输出证据的输入检查位于 [`JSONL Assistant 写入入口`](../../backend/internal/infra/jsonl/assistant.go)。任务队列序号到达上限时返回错误，禁止回绕为零。

## 保留的业务与安全约束

- 持久化读取严格拒绝非法或非 canonical 数据，不清洗、不补默认值，不改变已经审批的冻结参数。
- 状态转换、唯一性、跨实体引用归属、幂等冲突、预算单调性、不可变字段和最终输出证据保持约束。
- 当前审核与冻结参数摘要、审批来源、安全结论及生命周期之间的关系继续检查。模型审核失败或超时产生 uncertain，并进入人工审批。
- 执行前继续检查当前规则、Workspace 范围、符号链接、文件状态、权限及实际访问对象。未知副作用保持禁止自动重试。
- 为构造执行对象所必需的解析和错误处理继续保留，包括补丁解析、工具参数解码及代理 URL 解析。

## 验证与边界

已完成相关调用者检索、源码复核和 gofmt，并通过：

- `cd backend && go build ./...`
- `cd backend && go vet ./...`
- `cd backend && bash scripts/check-dependencies.sh`：全部 7 项分层依赖门禁通过。
- `git diff --check`：按仓库 Windows 换行格式检查，无空白错误。

遵守项目约定，未启动开发服务，未新增或运行自动化测试；运行时端到端行为未在本轮验证。

本地核查采用官方 [`code-review@claude-plugins-official` 审查指令](C:/Users/Administrator/.claude/plugins/marketplaces/claude-plugins-official/plugins/code-review/commands/code-review.md) 的规则对照、调用证据复核和置信度筛选原则。核查由主会话完成。
