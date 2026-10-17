# 模型协议适配约束

`backend/internal/infra/providers` 提供官方模型协议适配器共用的 HTTP 基础设施。每个协议独立使用 `protocol`、`encoder`、`provider`、`decoder`，不共享供应商状态机。

## 支持范围

- 支持文本、可选严格 JSON Schema 输出、客户端 Function Calling、可选 Usage、拒绝与截断终态；自定义 `stop_sequence` 视为正常结束。
- 无效用量只记录诊断，不终止正文流或更新统计。
- 文本模式消费正文，忽略 annotations/citations 元数据，不生成独立引用事件。
- 不支持多 choice、旧 `function_call`、MCP、服务端工具、多模态输入或私有协议扩展。

## 结构化输出

`model.ModelRequest.OutputSchema` 用于要求模型返回固定结构，未设置时不发送输出格式参数。`OutputSchema.Name` 必须为 1 至 64 字节的 ASCII 字母、数字、下划线或连字符；`Schema` 必须是根类型为 `object` 的 JSON Schema。适配器只读校验名称和根结构，具体支持的 Schema 关键字由 Provider 校验。

| 协议 | 请求参数 |
| --- | --- |
| OpenAI Chat Completions | `response_format: {type: "json_schema", json_schema: {name, schema, strict: true}}` |
| OpenAI Responses | `text: {format: {type: "json_schema", name, schema, strict: true}}` |
| Anthropic Messages | `output_config: {format: {type: "json_schema", schema}}`，与已有 `effort` 同时保留 |

`RiskReviewer` 使用名为 `risk_assessment` 的 Schema，要求 `verdict` 和 `reason` 两个字段均存在、禁止额外字段，`verdict` 只允许 `safe`、`dangerous`、`uncertain`。结果仍须经过本地严格解析及领域校验。Provider 不支持该能力、拒答、截断、超时或返回非法结果时，审核按 `uncertain` 进入人工审批，不自动降级为无 Schema 请求。

验证使用 `go build ./...`、`go vet ./...` 和依赖门禁；人工回归覆盖未设置 Schema、三种协议的格式参数、Messages 的格式与 effort 共存，以及审核正常结果和异常转人工的路径。

## 推理状态与历史重放

- Responses 使用 `store:false` 和 `reasoning.encrypted_content`，自行重放完整历史；只保存含 `encrypted_content` 的可重放 Reasoning Item。
- Messages 保留签名 Thinking 与 `redacted_thinking`；Thinking 模式、预算、Effort 能力来自 Task 已保存的模型配置，不能按模型名字猜测。
- `ReasoningState` 只由来源协议重放，不进入摘要或前端；实际提供的状态必须完整有效。
- Messages 仅在 `enabled` 模式下要求工具续传以原始 Thinking 块开头。

## 官方契约

- [OpenAI Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions)
- [OpenAI Responses 流式事件](https://developers.openai.com/api/reference/resources/responses/streaming-events)
- [OpenAI 结构化输出](https://developers.openai.com/api/docs/guides/structured-outputs)
- [Anthropic Messages 流式响应](https://platform.claude.com/docs/en/build-with-claude/streaming)
- [Anthropic 结构化输出](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)
