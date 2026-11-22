package anthropicmessages

import (
	"encoding/json"
	"errors"
	"fmt"
)

type contentBlock interface{ blockType() string }
type blockHeader struct {
	Type string `json:"type"`
}

func (b blockHeader) blockType() string { return b.Type }

type textBlock struct {
	blockHeader
	Text string `json:"text"`
	// 文本模式保留引用元数据，但只输出正文，不生成独立引用事件。
	Citations []json.RawMessage `json:"citations"`
}
type toolUseBlock struct {
	blockHeader
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
type thinkingBlock struct {
	blockHeader
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}
type redactedThinkingBlock struct {
	blockHeader
	Data string `json:"data"`
}
type contentBlockUnion struct {
	Value contentBlock
	Raw   map[string]json.RawMessage
}

// UnmarshalJSON 按 content_block.type 建立具体类型，签名与加密数据不被当作文本。
func (b *contentBlockUnion) UnmarshalJSON(data []byte) error {
	var header blockHeader
	if err := decodeObject(data, &header, "type"); err != nil {
		return err
	}
	var block contentBlock
	var required []string
	switch header.Type {
	case "text":
		block = &textBlock{}
		required = []string{"text"}
	case "tool_use":
		block = &toolUseBlock{}
		required = []string{"id", "name", "input"}
	case "thinking":
		block = &thinkingBlock{}
		required = []string{"thinking", "signature"}
	case "redacted_thinking":
		block = &redactedThinkingBlock{}
		required = []string{"data"}
	default:
		return fmt.Errorf("anthropic unsupported content block %q", header.Type)
	}
	if err := decodeObject(data, block, required...); err != nil {
		return err
	}
	switch value := block.(type) {
	case *toolUseBlock:
		var input map[string]json.RawMessage
		if value.ID == "" || value.Name == "" || json.Unmarshal(value.Input, &input) != nil || input == nil {
			return errors.New("anthropic invalid tool_use schema")
		}
	case *redactedThinkingBlock:
		if value.Data == "" {
			return errors.New("anthropic redacted_thinking missing data")
		}
	}
	if err := json.Unmarshal(data, &b.Raw); err != nil {
		return err
	}
	b.Value = block
	return nil
}

type contentDelta interface{ deltaType() string }
type deltaHeader struct {
	Type string `json:"type"`
}

func (d deltaHeader) deltaType() string { return d.Type }

type textDelta struct {
	deltaHeader
	Text string `json:"text"`
}
type thinkingDelta struct {
	deltaHeader
	Thinking string `json:"thinking"`
}
type signatureDelta struct {
	deltaHeader
	Signature string `json:"signature"`
}
type inputJSONDelta struct {
	deltaHeader
	PartialJSON string `json:"partial_json"`
}

// 文本模式识别引用增量以校验块归属，但不把引用元数据转换成正文。
type citationsDelta struct {
	deltaHeader
	Citation json.RawMessage `json:"citation"`
}
type contentDeltaUnion struct{ Value contentDelta }

// UnmarshalJSON 分派第二层 delta.type，未知增量可能损坏已注册块，必须报错。
func (d *contentDeltaUnion) UnmarshalJSON(data []byte) error {
	var header deltaHeader
	if err := decodeObject(data, &header, "type"); err != nil {
		return err
	}
	var delta contentDelta
	var field string
	switch header.Type {
	case "text_delta":
		delta, field = &textDelta{}, "text"
	case "thinking_delta":
		delta, field = &thinkingDelta{}, "thinking"
	case "signature_delta":
		delta, field = &signatureDelta{}, "signature"
	case "input_json_delta":
		delta, field = &inputJSONDelta{}, "partial_json"
	case "citations_delta":
		delta, field = &citationsDelta{}, "citation"
	default:
		return fmt.Errorf("anthropic unsupported content delta %q", header.Type)
	}
	if err := decodeObject(data, delta, field); err != nil {
		return err
	}
	d.Value = delta
	return nil
}
