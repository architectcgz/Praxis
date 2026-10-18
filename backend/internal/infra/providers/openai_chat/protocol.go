package openaichat

import (
	"encoding/json"
)

// 协议 DTO 仅描述官方 Chat Completions 文本与 Function Calling 字段。
type chatRequest struct {
	Model               string              `json:"model"`
	Messages            []chatMessage       `json:"messages"`
	N                   int                 `json:"n"`
	Stream              bool                `json:"stream"`
	StreamOptions       chatStreamOptions   `json:"stream_options"`
	ReasoningEffort     string              `json:"reasoning_effort,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens"`
	Tools               []chatTool          `json:"tools,omitempty"`
	PromptCacheKey      string              `json:"prompt_cache_key,omitempty"`
	ResponseFormat      *chatResponseFormat `json:"response_format,omitempty"`
}

type chatResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema chatJSONSchema `json:"json_schema"`
}
type chatJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
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
