package openaichat

import "encoding/json"

// 协议 DTO 仅描述官方 Chat Completions 文本与 Function Calling 字段。
type chatRequest struct {
	Model               string            `json:"model"`
	Messages            []chatMessage     `json:"messages"`
	N                   int               `json:"n"`
	Stream              bool              `json:"stream"`
	StreamOptions       chatStreamOptions `json:"stream_options"`
	ReasoningEffort     string            `json:"reasoning_effort,omitempty"`
	MaxCompletionTokens int               `json:"max_completion_tokens"`
	Tools               []chatTool        `json:"tools,omitempty"`
	PromptCacheKey      string            `json:"prompt_cache_key,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}
type chatTool struct {
	Type     string                 `json:"type"`
	Function chatFunctionDefinition `json:"function"`
}
type chatFunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
type chatMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}
type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}
type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type chatCompletionChunk struct {
	Choices []chatCompletionChoice `json:"choices"`
	Usage   *chatCompletionUsage   `json:"usage"`
	Error   *chatError             `json:"error"`
}
type chatError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
type chatCompletionChoice struct {
	Index        *int                `json:"index"`
	Delta        chatCompletionDelta `json:"delta"`
	FinishReason string              `json:"finish_reason"`
}
type chatCompletionDelta struct {
	Role         string                        `json:"role"`
	Content      string                        `json:"content"`
	Refusal      string                        `json:"refusal"`
	ToolCalls    []chatCompletionToolCallDelta `json:"tool_calls"`
	FunctionCall json.RawMessage               `json:"function_call"`
}
type chatCompletionToolCallDelta struct {
	Index    *int             `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}
type chatCompletionUsage struct {
	PromptTokens        *int64                   `json:"prompt_tokens"`
	CompletionTokens    *int64                   `json:"completion_tokens"`
	PromptTokensDetails *chatPromptTokensDetails `json:"prompt_tokens_details"`
}
type chatPromptTokensDetails struct {
	CachedTokens *int64 `json:"cached_tokens"`
}
