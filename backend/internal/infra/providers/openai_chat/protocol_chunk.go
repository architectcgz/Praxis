package openaichat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Chunk 保持官方结构；通过 ID 关联分片，content/refusal 等可空字段不补默认值。
type chatCompletionChunk struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           *int64                 `json:"created"`
	Model             string                 `json:"model"`
	Choices           []chatCompletionChoice `json:"choices"`
	Usage             *json.RawMessage       `json:"usage"`
	ServiceTier       *string                `json:"service_tier"`
	SystemFingerprint *string                `json:"system_fingerprint"`
	Obfuscation       *string                `json:"obfuscation"`
}
type chatCompletionChoice struct {
	Index        *int                 `json:"index"`
	Delta        *chatCompletionDelta `json:"delta"`
	FinishReason *string              `json:"finish_reason"`
}
type chatCompletionDelta struct {
	Role      *string                       `json:"role"`
	Content   *string                       `json:"content"`
	Refusal   *string                       `json:"refusal"`
	ToolCalls []chatCompletionToolCallDelta `json:"tool_calls"`
}
type chatCompletionToolCallDelta struct {
	Index    *int               `json:"index"`
	ID       *string            `json:"id"`
	Type     *string            `json:"type"`
	Function *chatFunctionDelta `json:"function"`
}
type chatFunctionDelta struct {
	Name      *string `json:"name"`
	Arguments *string `json:"arguments"`
}
type chatCompletionUsage struct {
	PromptTokens        *int64                   `json:"prompt_tokens"`
	CompletionTokens    *int64                   `json:"completion_tokens"`
	PromptTokensDetails *chatPromptTokensDetails `json:"prompt_tokens_details"`
}
type chatPromptTokensDetails struct {
	CachedTokens *int64 `json:"cached_tokens"`
}
type chatError struct {
	Type    string  `json:"type"`
	Message string  `json:"message"`
	Code    *string `json:"code"`
	Param   *string `json:"param"`
}

func decodeChunk(data []byte) (chatCompletionChunk, error) {
	var chunk chatCompletionChunk
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return chunk, err
	}
	if raw := envelope["error"]; len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var detail chatError
		if err := decodeObject(raw, &detail, "type", "message"); err != nil {
			return chunk, err
		}
		return chunk, fmt.Errorf("openai chat stream error (%s): %s", detail.Type, detail.Message)
	}
	if err := decodeObject(data, &chunk, "id", "object", "created", "model", "choices"); err != nil {
		return chunk, err
	}
	if chunk.Object != "chat.completion.chunk" || chunk.ID == "" || chunk.Model == "" || *chunk.Created < 0 {
		return chunk, errors.New("openai chat invalid chunk identity")
	}
	if len(chunk.Choices) > 1 {
		return chunk, errors.New("openai chat only supports choice index 0")
	}
	for _, choice := range chunk.Choices {
		if choice.Index == nil || *choice.Index != 0 || choice.Delta == nil {
			return chunk, errors.New("openai chat invalid choice schema")
		}
		if choice.Delta.Role != nil && *choice.Delta.Role != "assistant" {
			return chunk, errors.New("openai chat invalid delta role")
		}
		if choice.FinishReason != nil && mapStopReason(*choice.FinishReason) == "unknown" {
			return chunk, errors.New("openai chat unsupported finish_reason")
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.Index == nil || *call.Index < 0 || call.Type != nil && *call.Type != "function" || call.ID != nil && *call.ID == "" {
				return chunk, errors.New("openai chat invalid tool delta")
			}
		}
	}
	return chunk, nil
}

// UnmarshalJSON 在协议边界拒绝废弃接口和当前无法无损表示的音频，不将它们当作文本。
func (d *chatCompletionDelta) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"function_call", "audio"} {
		if raw := fields[name]; len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("openai chat unsupported delta field %q", name)
		}
	}
	if err := rejectNullFields(fields, "tool_calls"); err != nil {
		return err
	}
	type wire chatCompletionDelta
	return decodeObject(data, (*wire)(d))
}

// UnmarshalJSON 可选不代表可空，工具分片只允许省略尚未到达的字段。
func (d *chatCompletionToolCallDelta) UnmarshalJSON(data []byte) error {
	type wire chatCompletionToolCallDelta
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := rejectNullFields(fields, "id", "type", "function"); err != nil {
		return err
	}
	return decodeObject(data, (*wire)(d), "index")
}

// UnmarshalJSON 函数名与参数均可独立到达，但提供时必须是字符串。
func (d *chatFunctionDelta) UnmarshalJSON(data []byte) error {
	type wire chatFunctionDelta
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := rejectNullFields(fields, "name", "arguments"); err != nil {
		return err
	}
	return decodeObject(data, (*wire)(d))
}

func rejectNullFields(fields map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		if raw, exists := fields[name]; exists && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("openai chat null field %q", name)
		}
	}
	return nil
}

// decodeObject 校验必填且不可空字段，附加字段不影响已支持的协议语义。
func decodeObject(data []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("openai chat schema requires an object")
	}
	for _, name := range required {
		if raw := fields[name]; len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("openai chat missing or null field %q", name)
		}
	}
	return json.Unmarshal(data, target)
}
