package anthropicmessages

import (
	"encoding/json"
	"errors"
	"fmt"
)

type messageEvent interface{ eventType() string }
type messageEventHeader struct {
	Type string `json:"type"`
}

func (h messageEventHeader) eventType() string { return h.Type }

type messageStartEvent struct {
	messageEventHeader
	Message *messageSnapshot `json:"message"`
}
type contentBlockStartEvent struct {
	messageEventHeader
	Index        *int              `json:"index"`
	ContentBlock contentBlockUnion `json:"content_block"`
}
type contentBlockDeltaEvent struct {
	messageEventHeader
	Index *int              `json:"index"`
	Delta contentDeltaUnion `json:"delta"`
}
type contentBlockStopEvent struct {
	messageEventHeader
	Index *int `json:"index"`
}
type messageDeltaEvent struct {
	messageEventHeader
	Delta *messageDelta      `json:"delta"`
	Usage *messageDeltaUsage `json:"usage"`
}
type messageStopEvent struct{ messageEventHeader }
type pingEvent struct{ messageEventHeader }
type errorEvent struct {
	messageEventHeader
	Error *messageError `json:"error"`
}
type messageDelta struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}
type messageSnapshot struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Model        string              `json:"model"`
	Role         string              `json:"role"`
	Content      []contentBlockUnion `json:"content"`
	StopReason   *string             `json:"stop_reason"`
	StopSequence *string             `json:"stop_sequence"`
	Usage        *messageUsage       `json:"usage"`
}
type messageError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// tokenCounts 只用于累计计数；两个 Usage DTO 分别验证各自的官方必填字段。
type tokenCounts struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}
type messageUsage struct{ tokenCounts }
type messageDeltaUsage struct{ tokenCounts }

// UnmarshalJSON 首帧必须包含 input_tokens 和 output_tokens。
func (u *messageUsage) UnmarshalJSON(data []byte) error {
	type wire messageUsage
	return decodeObject(data, (*wire)(u), "input_tokens", "output_tokens")
}

// UnmarshalJSON 后续 Usage 是累计值，input_tokens 等字段可以缺失或为 null。
func (u *messageDeltaUsage) UnmarshalJSON(data []byte) error {
	type wire messageDeltaUsage
	return decodeObject(data, (*wire)(u), "output_tokens")
}

// UnmarshalJSON 验证完整 Message 身份，message_start 的生命周期由 Decoder 校验。
func (m *messageSnapshot) UnmarshalJSON(data []byte) error {
	type wire messageSnapshot
	if err := decodeObject(data, (*wire)(m), "id", "type", "model", "role", "content", "usage"); err != nil {
		return err
	}
	if !canonical(m.ID) || !canonical(m.Model) || m.Type != "message" || m.Role != "assistant" {
		return errors.New("anthropic invalid message identity")
	}
	return nil
}

// UnmarshalJSON 错误事件不允许缺失错误类型或错误消息。
func (e *messageError) UnmarshalJSON(data []byte) error {
	type wire messageError
	if err := decodeObject(data, (*wire)(e), "type", "message"); err != nil {
		return err
	}
	if e.Type == "" || e.Message == "" {
		return errors.New("anthropic empty error details")
	}
	return nil
}

func decodeMessageEvent(data []byte) (messageEvent, error) {
	var header messageEventHeader
	if err := decodeObject(data, &header, "type"); err != nil {
		return nil, err
	}
	if !canonical(header.Type) {
		return nil, errors.New("anthropic invalid event type")
	}
	var event messageEvent
	var required []string
	switch header.Type {
	case "message_start":
		event = &messageStartEvent{}
		required = []string{"message"}
	case "content_block_start":
		event = &contentBlockStartEvent{}
		required = []string{"index", "content_block"}
	case "content_block_delta":
		event = &contentBlockDeltaEvent{}
		required = []string{"index", "delta"}
	case "content_block_stop":
		event = &contentBlockStopEvent{}
		required = []string{"index"}
	case "message_delta":
		event = &messageDeltaEvent{}
		required = []string{"delta", "usage"}
	case "message_stop":
		event = &messageStopEvent{}
	case "ping":
		event = &pingEvent{}
	case "error":
		event = &errorEvent{}
		required = []string{"error"}
	default:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		for _, name := range []string{
			"index",
			"content_block",
			"delta",
			"message",
			"usage",
			"stop_reason",
			"error",
		} {
			if _, exists := fields[name]; exists {
				return nil, fmt.Errorf("anthropic unsupported semantic event %q", header.Type)
			}
		}
		return &header, nil
	}
	if err := decodeObject(data, event, required...); err != nil {
		return nil, err
	}
	var index *int
	switch e := event.(type) {
	case *contentBlockStartEvent:
		index = e.Index
	case *contentBlockDeltaEvent:
		index = e.Index
	case *contentBlockStopEvent:
		index = e.Index
	}
	if index != nil && *index < 0 {
		return nil, errors.New("anthropic negative content block index")
	}
	return event, nil
}
