# 模型协议适配约束

`backend/internal/infra/providers` 提供官方模型协议适配器共用的 HTTP 基础设施。每个协议独立使用 `protocol`、`encoder`、`provider`、`decoder`，不共享供应商状态机。

## 支持范围

- 支持文本、客户端 Function Calling、可选 Usage、拒绝与截断终态；自定义 `stop_sequence` 视为正常结束。
- 无效用量只记录诊断，不终止正文流或更新统计。
- 文本模式消费正文，忽略 annotations/citations 元数据，不生成独立引用事件。
- 不支持多 choice、旧 `function_call`、MCP、服务端工具、多模态输入或私有协议扩展。

## 推理状态与历史重放

- Responses 使用 `store:false` 和 `reasoning.encrypted_content`，自行重放完整历史；只保存含 `encrypted_content` 的可重放 Reasoning Item。
- Messages 保留签名 Thinking 与 `redacted_thinking`；Thinking 模式、预算、Effort 能力来自 Task 已保存的模型配置，不能按模型名字猜测。
- `ReasoningState` 只由来源协议重放，不进入摘要或前端；实际提供的状态必须完整有效。
- Messages 仅在 `enabled` 模式下要求工具续传以原始 Thinking 块开头。

## 官方契约

- [OpenAI Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions)
- [OpenAI Responses 流式事件](https://developers.openai.com/api/reference/resources/responses/streaming-events)
- [Anthropic Messages 流式响应](https://platform.claude.com/docs/en/build-with-claude/streaming)
