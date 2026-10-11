package openairesponses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 请求 DTO 与输出 DTO 分离，历史文本使用官方 EasyInputMessage。
type requestPayload struct {
	Model           string            `json:"model"`
	Input           []json.RawMessage `json:"input"`
	Stream          bool              `json:"stream"`
	Store           bool              `json:"store"`
	Include         []string          `json:"include"`
	Reasoning       *reasoningConfig  `json:"reasoning,omitempty"`
	MaxOutputTokens int               `json:"max_output_tokens"`
	Tools           []responseTool    `json:"tools,omitempty"`
	PromptCacheKey  string            `json:"prompt_cache_key,omitempty"`
}
type reasoningConfig struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}
type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
type inputMessage struct {
	Type    string      `json:"type"`
	Role    string      `json:"role"`
	Content []inputText `json:"content"`
}
type inputText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type inputFunctionCall struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type inputFunctionOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// 快照保留官方必填核心字段；未消费的配置联合类型不用于推断输出语义。
type responseSnapshot struct {
	ID                string                     `json:"id"`
	Object            string                     `json:"object"`
	CreatedAt         float64                    `json:"created_at"`
	Model             string                     `json:"model"`
	ParallelToolCalls bool                       `json:"parallel_tool_calls"`
	ToolChoice        json.RawMessage            `json:"tool_choice"`
	Tools             []json.RawMessage          `json:"tools"`
	Status            *string                    `json:"status"`
	Output            []responseOutput           `json:"output"`
	IncompleteDetails *responseIncompleteDetails `json:"incomplete_details"`
	Error             *responseError             `json:"error"`
	Usage             *responseUsage             `json:"usage"`
}
type responseIncompleteDetails struct {
	Reason *string `json:"reason"`
}
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type responseUsage struct {
	InputTokens         *int64                       `json:"input_tokens"`
	OutputTokens        *int64                       `json:"output_tokens"`
	TotalTokens         *int64                       `json:"total_tokens"`
	InputTokensDetails  *responseInputTokensDetails  `json:"input_tokens_details"`
	OutputTokensDetails *responseOutputTokensDetails `json:"output_tokens_details"`
}
type responseInputTokensDetails struct {
	CachedTokens *int64 `json:"cached_tokens"`
}
type responseOutputTokensDetails struct {
	ReasoningTokens *int64 `json:"reasoning_tokens"`
}

// UnmarshalJSON 校验快照身份、核心必填字段和完整 Usage，不静默补默认值。
func (r *responseSnapshot) UnmarshalJSON(data []byte) error {
	type wire responseSnapshot
	if err := decodeObject(data, (*wire)(r), "id", "object", "created_at", "model", "output", "parallel_tool_calls", "tool_choice", "tools"); err != nil {
		return err
	}
	if !canonical(r.ID) || !canonical(r.Model) || r.Object != "response" || r.CreatedAt < 0 {
		return errors.New("responses invalid snapshot identity")
	}
	var choice string
	if json.Unmarshal(r.ToolChoice, &choice) == nil {
		if choice != "auto" && choice != "none" && choice != "required" {
			return errors.New("responses invalid tool_choice")
		}
	} else {
		var header struct {
			Type string `json:"type"`
		}
		if err := decodeObject(r.ToolChoice, &header, "type"); err != nil {
			return err
		}
		if !canonical(header.Type) {
			return errors.New("responses invalid tool_choice type")
		}
	}
	for _, raw := range r.Tools {
		var header struct {
			Type string `json:"type"`
		}
		if err := decodeObject(raw, &header, "type"); err != nil {
			return err
		}
		if !canonical(header.Type) {
			return errors.New("responses invalid tool type")
		}
	}
	if r.Status != nil {
		switch *r.Status {
		case "queued", "in_progress", "completed", "failed", "incomplete", "cancelled":
		default:
			return errors.New("responses invalid status")
		}
	}
	if r.Error != nil && (r.Error.Code == "" || r.Error.Message == "") {
		return errors.New("responses invalid error details")
	}
	if u := r.Usage; u != nil {
		if u.InputTokens == nil || u.OutputTokens == nil || u.TotalTokens == nil || u.InputTokensDetails == nil || u.InputTokensDetails.CachedTokens == nil || u.OutputTokensDetails == nil || u.OutputTokensDetails.ReasoningTokens == nil {
			return errors.New("responses incomplete usage schema")
		}
		if *u.InputTokens < 0 || *u.OutputTokens < 0 || *u.TotalTokens < *u.InputTokens || *u.TotalTokens-*u.InputTokens != *u.OutputTokens || *u.InputTokensDetails.CachedTokens < 0 || *u.InputTokensDetails.CachedTokens > *u.InputTokens || *u.OutputTokensDetails.ReasoningTokens < 0 || *u.OutputTokensDetails.ReasoningTokens > *u.OutputTokens {
			return errors.New("responses invalid usage")
		}
	}
	return nil
}

func canonical(value string) bool { return value != "" && value == strings.TrimSpace(value) }

// decodeObject 只验证明确的必填字段，允许官方增加附加字段；null 不代替必填零值。
func decodeObject(data []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("responses schema requires an object")
	}
	for _, name := range required {
		if raw := fields[name]; len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("responses missing or null field %q", name)
		}
	}
	return json.Unmarshal(data, target)
}
