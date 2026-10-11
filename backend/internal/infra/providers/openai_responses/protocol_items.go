package openairesponses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type responseOutputValue interface{ outputIdentity() (string, string) }

// responseOutput 保存已分派的联合值与原始数据；推理续传只使用 done 的原始载荷。
type responseOutput struct {
	Value responseOutputValue
	Raw   json.RawMessage
}
type responseMessageOutput struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Status  string         `json:"status"`
	Content []responsePart `json:"content"`
}
type responseReasoningOutput struct {
	ID               string                  `json:"id"`
	Type             string                  `json:"type"`
	Status           *string                 `json:"status"`
	Summary          []responseSummaryText   `json:"summary"`
	Content          []responseReasoningText `json:"content"`
	EncryptedContent *string                 `json:"encrypted_content"`
}
type responseFunctionCallOutput struct {
	ID        *string `json:"id"`
	Type      string  `json:"type"`
	CallID    string  `json:"call_id"`
	Name      string  `json:"name"`
	Arguments string  `json:"arguments"`
	Status    *string `json:"status"`
}

func (i *responseMessageOutput) outputIdentity() (string, string)   { return i.ID, i.Type }
func (i *responseReasoningOutput) outputIdentity() (string, string) { return i.ID, i.Type }
func (i *responseFunctionCallOutput) outputIdentity() (string, string) {
	if i.ID == nil {
		return "", i.Type
	}
	return *i.ID, i.Type
}

// UnmarshalJSON 根据 item.type 解码独立 Schema，未知功能不伪装为正常输出。
func (i *responseOutput) UnmarshalJSON(data []byte) error {
	var header struct {
		Type string `json:"type"`
	}
	if err := decodeObject(data, &header, "type"); err != nil {
		return err
	}
	var status *string
	switch header.Type {
	case "message":
		var item responseMessageOutput
		if err := decodeObject(data, &item, "id", "role", "status", "content"); err != nil {
			return err
		}
		if !canonical(item.ID) || item.Role != "assistant" {
			return errors.New("responses invalid output message identity")
		}
		for _, part := range item.Content {
			kind, _ := part.Value.partText()
			if kind != "output_text" && kind != "refusal" {
				return errors.New("responses message contains a non-message part")
			}
		}
		status, i.Value = &item.Status, &item
	case "reasoning":
		var item responseReasoningOutput
		if err := decodeObject(data, &item, "id", "summary"); err != nil {
			return err
		}
		if !canonical(item.ID) {
			return errors.New("responses invalid reasoning ID")
		}
		status, i.Value = item.Status, &item
	case "function_call":
		var item responseFunctionCallOutput
		if err := decodeObject(data, &item, "call_id", "name", "arguments"); err != nil {
			return err
		}
		if !canonical(item.CallID) || !canonical(item.Name) || item.ID != nil && !canonical(*item.ID) {
			return errors.New("responses invalid function identity")
		}
		status, i.Value = item.Status, &item
	default:
		return fmt.Errorf("responses unsupported output item type %q", header.Type)
	}
	if status != nil && *status != "in_progress" && *status != "completed" && *status != "incomplete" {
		return errors.New("responses invalid output item status")
	}
	i.Raw = bytes.Clone(data)
	return nil
}

type responsePartValue interface{ partText() (string, string) }
type responsePart struct{ Value responsePartValue }
type responseOutputText struct {
	Type        string                `json:"type"`
	Text        string                `json:"text"`
	Annotations []json.RawMessage     `json:"annotations"`
	Logprobs    []responseTextLogprob `json:"logprobs"`
}
type responseTextLogprob struct {
	Token       string                   `json:"token"`
	Bytes       []int                    `json:"bytes"`
	Logprob     float64                  `json:"logprob"`
	TopLogprobs []responseTextTopLogprob `json:"top_logprobs"`
}
type responseTextTopLogprob struct {
	Token   string  `json:"token"`
	Bytes   []int   `json:"bytes"`
	Logprob float64 `json:"logprob"`
}
type responseRefusal struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}
type responseSummaryText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type responseReasoningText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (p *responseOutputText) partText() (string, string)    { return p.Type, p.Text }
func (p *responseRefusal) partText() (string, string)       { return p.Type, p.Refusal }
func (p *responseSummaryText) partText() (string, string)   { return p.Type, p.Text }
func (p *responseReasoningText) partText() (string, string) { return p.Type, p.Text }

// UnmarshalJSON 引用没有内部表示，显式报不支持而不是丢弃引用语义。
func (p *responseOutputText) UnmarshalJSON(data []byte) error {
	type wire responseOutputText
	if err := decodeObject(data, (*wire)(p), "type", "text", "annotations"); err != nil {
		return err
	}
	if len(p.Annotations) != 0 {
		return errors.New("responses output annotations are not supported")
	}
	return nil
}

// UnmarshalJSON 验证正文日志概率的完整字段，空候选数组是合法值。
func (p *responseTextLogprob) UnmarshalJSON(data []byte) error {
	type wire responseTextLogprob
	return decodeObject(data, (*wire)(p), "token", "bytes", "logprob", "top_logprobs")
}

// UnmarshalJSON 验证正文候选 token，缺失概率不默认为零。
func (p *responseTextTopLogprob) UnmarshalJSON(data []byte) error {
	type wire responseTextTopLogprob
	return decodeObject(data, (*wire)(p), "token", "bytes", "logprob")
}

// UnmarshalJSON 不把拒绝、摘要和原始推理文本混入同一个字段集合。
func (p *responsePart) UnmarshalJSON(data []byte) error {
	var header struct {
		Type string `json:"type"`
	}
	if err := decodeObject(data, &header, "type"); err != nil {
		return err
	}
	var target responsePartValue
	required := []string{"type", "text"}
	switch header.Type {
	case "output_text":
		target = &responseOutputText{}
		required = append(required, "annotations")
	case "refusal":
		target = &responseRefusal{}
		required = []string{"type", "refusal"}
	case "reasoning_text":
		target = &responseReasoningText{}
	default:
		return fmt.Errorf("responses unsupported content part %q", header.Type)
	}
	if err := decodeObject(data, target, required...); err != nil {
		return err
	}
	p.Value = target
	return nil
}

// UnmarshalJSON 验证必填文本和摘要的固定 discriminator，空字符串是合法内容。
func (p *responseSummaryText) UnmarshalJSON(data []byte) error {
	type wire responseSummaryText
	if err := decodeObject(data, (*wire)(p), "type", "text"); err != nil {
		return err
	}
	if p.Type != "summary_text" {
		return errors.New("responses invalid summary part type")
	}
	return nil
}

// UnmarshalJSON 验证原始推理文本，避免摘要和正文索引被错误混用。
func (p *responseReasoningText) UnmarshalJSON(data []byte) error {
	type wire responseReasoningText
	if err := decodeObject(data, (*wire)(p), "type", "text"); err != nil {
		return err
	}
	if p.Type != "reasoning_text" {
		return errors.New("responses invalid reasoning part type")
	}
	return nil
}
