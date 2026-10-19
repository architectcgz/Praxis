# 模型协议契约

本文只定义模型通信协议的支持范围、请求与响应、结构化输出、流式事件及跨协议历史重放约束，不描述项目代码组织或业务流程。

## 支持范围

- 支持 OpenAI Chat Completions、OpenAI Responses 和 Anthropic Messages。
- 支持文本输入和输出、可选的严格 JSON Schema 输出、客户端 Function Calling、可选 Usage，以及拒绝、截断和正常结束信号。
- 自定义 `stop_sequence` 命中属于正常结束，不等同于请求错误。
- 文本模式只消费响应正文；`annotations`、`citations` 等附加元数据不生成独立引用事件。
- 不支持多 choice、旧式 `function_call`、MCP、服务端工具、多模态输入或私有协议扩展。

## 结构化输出

未要求结构化输出时，不发送 JSON Schema 格式参数。要求结构化输出时，Schema 名称使用 1 至 64 字节的 ASCII 字母、数字、下划线或连字符，根 JSON Schema 的 `type` 必须为 `object`。具体 Schema 关键字是否可用，以目标协议及 Provider 的支持能力为准。

| 协议 | 请求参数 |
| --- | --- |
| OpenAI Chat Completions | `response_format: {type: "json_schema", json_schema: {name, schema, strict: true}}` |
| OpenAI Responses | `text: {format: {type: "json_schema", name, schema, strict: true}}` |
| Anthropic Messages | `output_config: {format: {type: "json_schema", schema}}`；与已设置的 `effort` 同时保留 |

结构化响应仍可能出现拒答、截断或无效内容；启用了 Schema 不等于响应一定满足 Schema。

## 流式响应与结束信号

- 按各协议定义的事件类型和字段结构解析流，不把不同协议的事件 Schema 混用。
- 区分正常结束、拒绝、截断和传输或协议错误；正常的停止序列不应转成错误。
- Usage 是可选数据；缺失 Usage 不影响正文消费，无效 Usage 不更新统计，也不使有效正文流失败。
- 工具请求必须使用结构化 Function Calling 数据，不把正文文本当作工具调用。

## 推理状态与续接

- **OpenAI Responses**：使用 `store: false`，通过 `reasoning.encrypted_content` 接收可重放的推理状态；只保留包含 `encrypted_content` 的 Reasoning Item。
- **Anthropic Messages**：保留签名 Thinking 和 `redacted_thinking` 原始载荷。Thinking 模式、预算与 Effort 依据请求配置，不按模型名称推断。
- **OpenAI Chat Completions**：不重放上述 opaque 推理状态。
- opaque 推理状态只有在 Provider、协议及 Model 标识均与原始响应匹配时才原样重放；来源不匹配时从本次请求中忽略，不转换成正文文本，也不跨协议迁移。
- Anthropic Thinking 续接缺少前导 Thinking 块时，仍按请求发送；是否接受或降级由目标 Provider 决定。
- 文本、工具请求和回执保持跨协议可转换；opaque 推理状态不作跨协议转换。

## 工具调用 ID

- 各协议的工具请求及对应回执必须使用同一调用 ID。
- ID 为 1 至 64 字节的 ASCII 字母、数字、下划线或连字符时，可直接用于请求编码。
- 不满足目标协议格式的 ID 使用当前请求内无碰撞的映射值；请求及回执复用映射，历史中的原始 ID 保持不变。

## 官方协议参考

- [OpenAI Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions)
- [OpenAI Responses 流式事件](https://developers.openai.com/api/reference/resources/responses/streaming-events)
- [OpenAI 结构化输出](https://developers.openai.com/api/docs/guides/structured-outputs)
- [Anthropic Messages 流式响应](https://platform.claude.com/docs/en/build-with-claude/streaming)
- [Anthropic 结构化输出](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)
