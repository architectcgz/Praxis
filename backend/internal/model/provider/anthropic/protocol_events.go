package anthropicmessages

import (
	"encoding/json"
	"errors"
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
	Delta *messageDelta    `json:"delta"`
	Usage *json.RawMessage `json:"usage"`
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
	Usage        *json.RawMessage    `json:"usage"`
}
type messageError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// tokenCounts 只承载实际消费的累计计数，Decoder 独立处理统计异常。
type tokenCounts struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

// UnmarshalJSON 验证完整 Message 身份，message_start 的生命周期由 Decoder 校验。
func (m *messageSnapshot) UnmarshalJSON(data []byte) error {
	type wire messageSnapshot
	if err := decodeObject(data, (*wire)(m), "id", "type", "model", "role", "content"); err != nil {
		return err
	}
	if m.ID == "" || m.Model == "" || m.Type != "message" || m.Role != "assistant" {
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
	if header.Type == "" {
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
		required = []string{"delta"}
	case "message_stop":
		event = &messageStopEvent{}
	case "ping":
		event = &pingEvent{}
	case "error":
		event = &errorEvent{}
		required = []string{"error"}
	default:
		// 顶层新增事件按协议扩展忽略，不据字段名称推断其语义或修改现有块。
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
