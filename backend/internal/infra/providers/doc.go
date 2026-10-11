// Package providers 定义官方模型协议适配器共用的 HTTP 基础设施。
// 每个协议独立使用 protocol/encoder/provider/decoder，不共享供应商状态机。
//
// 支持范围：文本、客户端 Function Calling、Usage、拒绝与截断终态；
// Responses 保留官方 Reasoning Item，Messages 保留签名 Thinking 与 redacted_thinking。
// 不支持多 choice、旧 function_call、MCP、服务端工具、多模态输入或私有协议扩展。
//
// Responses 使用 store:false 和 reasoning.encrypted_content，自行重放完整历史；
// Messages 的 Thinking 模式、预算、Effort 能力来自 Task 冻结快照，不能按模型名字猜测。
// Opaque State 只由来源协议重放，不进入摘要或前端；缺失或不一致时显式报错。
//
// 官方契约：
// https://developers.openai.com/api/reference/resources/chat/subresources/completions
// https://developers.openai.com/api/reference/resources/responses/streaming-events
// https://platform.claude.com/docs/en/build-with-claude/streaming
package providers
