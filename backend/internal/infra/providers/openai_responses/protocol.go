package openairesponses

import "encoding/json"

// 协议 DTO 只覆盖官方文本、拒绝、Reasoning Item 与 Function Calling。
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
type responseItem struct {
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Content   []responseContentPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Output    *string               `json:"output,omitempty"`
}
type responseContentPart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}
type responseOutputItem struct {
	ID               string                `json:"id"`
	Type             string                `json:"type"`
	Status           string                `json:"status"`
	Role             string                `json:"role"`
	CallID           string                `json:"call_id"`
	Name             string                `json:"name"`
	Arguments        string                `json:"arguments"`
	Summary          []responseContentPart `json:"summary"`
	Content          []responseContentPart `json:"content"`
	EncryptedContent string                `json:"encrypted_content"`
}
type responseStreamEvent struct {
	Type           string              `json:"type"`
	SequenceNumber *int64              `json:"sequence_number"`
	OutputIndex    *int                `json:"output_index"`
	ContentIndex   *int                `json:"content_index"`
	SummaryIndex   *int                `json:"summary_index"`
	ItemID         string              `json:"item_id"`
	Item           json.RawMessage     `json:"item"`
	Delta          string              `json:"delta"`
	Text           string              `json:"text"`
	Refusal        string              `json:"refusal"`
	Arguments      string              `json:"arguments"`
	Part           responseContentPart `json:"part"`
	Response       *responseSnapshot   `json:"response"`
	Code           string              `json:"code"`
	Message        string              `json:"message"`
}
type responseSnapshot struct {
	ID                string                     `json:"id"`
	Status            string                     `json:"status"`
	Output            []json.RawMessage          `json:"output"`
	IncompleteDetails *responseIncompleteDetails `json:"incomplete_details"`
	Error             *responseError             `json:"error"`
	Usage             *responseUsage             `json:"usage"`
}
type responseIncompleteDetails struct {
	Reason string `json:"reason"`
}
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type responseUsage struct {
	InputTokens        *int64                      `json:"input_tokens"`
	OutputTokens       *int64                      `json:"output_tokens"`
	InputTokensDetails *responseInputTokensDetails `json:"input_tokens_details"`
}
type responseInputTokensDetails struct {
	CachedTokens *int64 `json:"cached_tokens"`
}
