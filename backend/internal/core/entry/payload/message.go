package payload

import (
	"encoding/json"
	"errors"
	"praxis/internal/core/entry/record"
	"praxis/internal/core/entry/role"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
)

type Message struct {
	Role     role.Role      `json:"role"`
	Content  record.Content `json:"content"`
	Response *ModelResponse `json:"response,omitempty"`
}

func (Message) isPayload() {}

// ModelResponse 只描述产生本条消息内容的模型响应。
type ModelResponse struct {
	RequestID  identity.ModelRequestID `json:"requestId"`
	StopReason string                  `json:"stopReason,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Timing     *timing.Record          `json:"timing,omitempty"`
}

// Summary 作为历史资料进入模型上下文，不属于某一条对话消息。
type Summary struct {
	Text       string                    `json:"text"`
	RequestIDs []identity.ModelRequestID `json:"requestIds"`
	Timing     *timing.Record            `json:"timing,omitempty"`
}

func (Summary) isPayload() {}

func decodeSummary(data json.RawMessage) (Payload, error) {
	var decoded struct {
		Summary
		Text    *string        `json:"text"`
		Content record.Content `json:"content"`
	}
	if err := decodeFields(data, &decoded); err != nil {
		return nil, err
	}
	switch {
	case decoded.Text != nil && decoded.Content == nil:
		decoded.Summary.Text = *decoded.Text
	case decoded.Text == nil && len(decoded.Content) == 1:
		text, ok := decoded.Content[0].(record.TextBlock)
		if !ok {
			return nil, errors.New("summary content requires a text block")
		}
		decoded.Summary.Text = text.Text
	default:
		return nil, errors.New("summary requires exactly one text value")
	}
	return decoded.Summary, nil
}
