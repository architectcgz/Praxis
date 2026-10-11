package openairesponses

import (
	"encoding/json"
	"errors"
	"fmt"
)

type responseEvent interface{ eventHeader() responseEventHeader }
type responseEventHeader struct {
	Type           string `json:"type"`
	SequenceNumber *int64 `json:"sequence_number"`
}

func (h responseEventHeader) eventHeader() responseEventHeader { return h }

type responseItemReference struct {
	ItemID      string `json:"item_id"`
	OutputIndex *int   `json:"output_index"`
}
type responseContentReference struct {
	responseItemReference
	ContentIndex *int `json:"content_index"`
}
type responseSummaryReference struct {
	responseItemReference
	SummaryIndex *int `json:"summary_index"`
}

func (r responseItemReference) validateReference() error {
	if r.OutputIndex == nil || *r.OutputIndex < 0 || !canonical(r.ItemID) {
		return errors.New("responses invalid item reference")
	}
	return nil
}
func (r responseContentReference) validateReference() error {
	if err := r.responseItemReference.validateReference(); err != nil {
		return err
	}
	if r.ContentIndex == nil || *r.ContentIndex < 0 {
		return errors.New("responses invalid content_index")
	}
	return nil
}
func (r responseSummaryReference) validateReference() error {
	if err := r.responseItemReference.validateReference(); err != nil {
		return err
	}
	if r.SummaryIndex == nil || *r.SummaryIndex < 0 {
		return errors.New("responses invalid summary_index")
	}
	return nil
}

type responseCreatedEvent struct {
	responseEventHeader
	Response *responseSnapshot `json:"response"`
}
type responseInProgressEvent struct {
	responseEventHeader
	Response *responseSnapshot `json:"response"`
}
type responseCompletedEvent struct {
	responseEventHeader
	Response *responseSnapshot `json:"response"`
}
type responseFailedEvent struct {
	responseEventHeader
	Response *responseSnapshot `json:"response"`
}
type responseIncompleteEvent struct {
	responseEventHeader
	Response *responseSnapshot `json:"response"`
}
type responseOutputItemAddedEvent struct {
	responseEventHeader
	OutputIndex *int           `json:"output_index"`
	Item        responseOutput `json:"item"`
}
type responseOutputItemDoneEvent struct {
	responseEventHeader
	OutputIndex *int           `json:"output_index"`
	Item        responseOutput `json:"item"`
}
type responseContentPartAddedEvent struct {
	responseEventHeader
	responseContentReference
	Part responsePart `json:"part"`
}
type responseContentPartDoneEvent struct {
	responseEventHeader
	responseContentReference
	Part responsePart `json:"part"`
}
type responseOutputTextDeltaEvent struct {
	responseEventHeader
	responseContentReference
	Delta       string                 `json:"delta"`
	Logprobs    []responseEventLogprob `json:"logprobs"`
	Obfuscation *string                `json:"obfuscation"`
}
type responseOutputTextDoneEvent struct {
	responseEventHeader
	responseContentReference
	Text     string                 `json:"text"`
	Logprobs []responseEventLogprob `json:"logprobs"`
}
type responseRefusalDeltaEvent struct {
	responseEventHeader
	responseContentReference
	Delta string `json:"delta"`
}
type responseRefusalDoneEvent struct {
	responseEventHeader
	responseContentReference
	Refusal string `json:"refusal"`
}
type responseReasoningSummaryPartAddedEvent struct {
	responseEventHeader
	responseSummaryReference
	Part responseSummaryText `json:"part"`
}
type responseReasoningSummaryPartDoneEvent struct {
	responseEventHeader
	responseSummaryReference
	Part   responseSummaryText `json:"part"`
	Status *string             `json:"status"`
}
type responseReasoningSummaryTextDeltaEvent struct {
	responseEventHeader
	responseSummaryReference
	Delta       string  `json:"delta"`
	Obfuscation *string `json:"obfuscation"`
}
type responseReasoningSummaryTextDoneEvent struct {
	responseEventHeader
	responseSummaryReference
	Text string `json:"text"`
}
type responseReasoningTextDeltaEvent struct {
	responseEventHeader
	responseContentReference
	Delta       string  `json:"delta"`
	Obfuscation *string `json:"obfuscation"`
}
type responseReasoningTextDoneEvent struct {
	responseEventHeader
	responseContentReference
	Text string `json:"text"`
}
type responseFunctionCallArgumentsDeltaEvent struct {
	responseEventHeader
	responseItemReference
	Delta       string  `json:"delta"`
	Obfuscation *string `json:"obfuscation"`
}
type responseFunctionCallArgumentsDoneEvent struct {
	responseEventHeader
	responseItemReference
	Arguments string `json:"arguments"`
}
type responseErrorEvent struct {
	responseEventHeader
	Code    *string `json:"code"`
	Message string  `json:"message"`
	Param   *string `json:"param"`
}
type responseEventLogprob struct {
	Token       string                    `json:"token"`
	Logprob     float64                   `json:"logprob"`
	TopLogprobs []responseEventTopLogprob `json:"top_logprobs"`
}
type responseEventTopLogprob struct {
	Token   *string  `json:"token"`
	Logprob *float64 `json:"logprob"`
}

// UnmarshalJSON 流事件的日志概率与正文日志概率不同，不要求 bytes 或 top_logprobs。
func (p *responseEventLogprob) UnmarshalJSON(data []byte) error {
	type wire responseEventLogprob
	return decodeObject(data, (*wire)(p), "token", "logprob")
}

// decodeResponseEvent 先分派顶层类型，再验证该类型的必填字段；索引零值合法。
// 未知事件仅在不携带输出、块或终态数据时跳过，绝不修改已注册输出。
func decodeResponseEvent(data []byte) (responseEvent, error) {
	var header responseEventHeader
	if err := decodeObject(data, &header, "type"); err != nil {
		return nil, err
	}
	if !canonical(header.Type) {
		return nil, errors.New("responses invalid event type")
	}
	var event responseEvent
	var required []string
	switch header.Type {
	case "response.created":
		event = &responseCreatedEvent{}
		required = []string{"response"}
	case "response.in_progress":
		event = &responseInProgressEvent{}
		required = []string{"response"}
	case "response.completed":
		event = &responseCompletedEvent{}
		required = []string{"response"}
	case "response.failed":
		event = &responseFailedEvent{}
		required = []string{"response"}
	case "response.incomplete":
		event = &responseIncompleteEvent{}
		required = []string{"response"}
	case "response.output_item.added":
		event = &responseOutputItemAddedEvent{}
		required = []string{"output_index", "item"}
	case "response.output_item.done":
		event = &responseOutputItemDoneEvent{}
		required = []string{"output_index", "item"}
	case "response.content_part.added":
		event = &responseContentPartAddedEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"part",
		}
	case "response.content_part.done":
		event = &responseContentPartDoneEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"part",
		}
	case "response.output_text.delta":
		event = &responseOutputTextDeltaEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"delta",
			"logprobs",
		}
	case "response.output_text.done":
		event = &responseOutputTextDoneEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"text",
			"logprobs",
		}
	case "response.refusal.delta":
		event = &responseRefusalDeltaEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"delta",
		}
	case "response.refusal.done":
		event = &responseRefusalDoneEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"refusal",
		}
	case "response.reasoning_summary_part.added":
		event = &responseReasoningSummaryPartAddedEvent{}
		required = []string{
			"item_id",
			"output_index",
			"summary_index",
			"part",
		}
	case "response.reasoning_summary_part.done":
		event = &responseReasoningSummaryPartDoneEvent{}
		required = []string{
			"item_id",
			"output_index",
			"summary_index",
			"part",
		}
	case "response.reasoning_summary_text.delta":
		event = &responseReasoningSummaryTextDeltaEvent{}
		required = []string{
			"item_id",
			"output_index",
			"summary_index",
			"delta",
		}
	case "response.reasoning_summary_text.done":
		event = &responseReasoningSummaryTextDoneEvent{}
		required = []string{
			"item_id",
			"output_index",
			"summary_index",
			"text",
		}
	case "response.reasoning_text.delta":
		event = &responseReasoningTextDeltaEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"delta",
		}
	case "response.reasoning_text.done":
		event = &responseReasoningTextDoneEvent{}
		required = []string{
			"item_id",
			"output_index",
			"content_index",
			"text",
		}
	case "response.function_call_arguments.delta":
		event = &responseFunctionCallArgumentsDeltaEvent{}
		required = []string{"item_id", "output_index", "delta"}
	case "response.function_call_arguments.done":
		event = &responseFunctionCallArgumentsDoneEvent{}
		required = []string{"item_id", "output_index", "arguments"}
	case "error":
		event = &responseErrorEvent{}
		required = []string{"message"}
	default:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		for _, name := range []string{
			"item_id",
			"output_index",
			"content_index",
			"summary_index",
			"item",
			"part",
			"delta",
			"response",
			"arguments",
			"text",
			"refusal",
		} {
			if _, exists := fields[name]; exists {
				return nil, fmt.Errorf("responses unsupported semantic event %q", header.Type)
			}
		}
		return &header, nil
	}
	required = append(required, "type", "sequence_number")
	if err := decodeObject(data, event, required...); err != nil {
		return nil, err
	}
	if *event.eventHeader().SequenceNumber < 0 {
		return nil, errors.New("responses negative sequence_number")
	}
	if reference, ok := event.(interface{ validateReference() error }); ok {
		if err := reference.validateReference(); err != nil {
			return nil, err
		}
	}
	var outputIndex *int
	switch e := event.(type) {
	case *responseOutputItemAddedEvent:
		outputIndex = e.OutputIndex
	case *responseOutputItemDoneEvent:
		outputIndex = e.OutputIndex
	}
	if outputIndex != nil && *outputIndex < 0 {
		return nil, errors.New("responses negative output_index")
	}
	if e, ok := event.(*responseReasoningSummaryPartDoneEvent); ok && e.Status != nil && *e.Status != "incomplete" {
		return nil, errors.New("responses invalid summary completion status")
	}
	return event, nil
}
