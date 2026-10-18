package anthropicmessages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// 请求 DTO 独立于响应 Content Block；重放签名载荷时保留原始 JSON。
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
	Effort string               `json:"effort,omitempty"`
	Format *anthropicJSONSchema `json:"format,omitempty"`
}
type anthropicJSONSchema struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
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
type requestTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type requestToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
type requestToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitzero"`
}

// decodeObject 保留未知附加字段的扩展空间，但不补齐缺失或 null 的必填字段。
func decodeObject(data []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("anthropic schema requires an object")
	}
	for _, name := range required {
		if raw := fields[name]; len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("anthropic missing or null field %q", name)
		}
	}
	return json.Unmarshal(data, target)
}
