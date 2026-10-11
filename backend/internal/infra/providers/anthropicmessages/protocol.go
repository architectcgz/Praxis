package anthropicmessages

import "encoding/json"

// 协议 DTO 仅描述官方 Messages API；不把未知 block 解释成普通文本。
type anthropicRequest struct {
	Model        string                    `json:"model"`
	Messages     []anthropicMessage        `json:"messages"`
	System       string                    `json:"system,omitempty"`
	Stream       bool                      `json:"stream"`
	MaxTokens    int                       `json:"max_tokens"`
	CacheControl anthropicCacheControl     `json:"cache_control"`
	Thinking     *anthropicThinking        `json:"thinking,omitempty"`
	OutputConfig *anthropicOutputConfig    `json:"output_config,omitempty"`
	Tools        []anthropicToolDefinition `json:"tools,omitempty"`
}
type anthropicCacheControl struct {
	Type string `json:"type"`
}
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitzero"`
}
type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}
type anthropicToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}
type anthropicMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}
type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   *string         `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitzero"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
}
type anthropicStreamEvent struct {
	Type         string                 `json:"type"`
	Index        *int                   `json:"index"`
	Usage        *anthropicMessageUsage `json:"usage"`
	Delta        anthropicDelta         `json:"delta"`
	ContentBlock json.RawMessage        `json:"content_block"`
	Message      *anthropicMessageStart `json:"message"`
	Error        *anthropicError        `json:"error"`
}
type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}
type anthropicMessageStart struct {
	ID      string                 `json:"id"`
	Role    string                 `json:"role"`
	Content []json.RawMessage      `json:"content"`
	Usage   *anthropicMessageUsage `json:"usage"`
}
type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
type anthropicMessageUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}
