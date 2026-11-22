package bindings

import (
	"praxis/internal/core/entry"
	sessionmodel "praxis/internal/core/session"
	"time"
)

type MessageKind string

const (
	MessageChat    MessageKind = "message"
	MessageSummary MessageKind = "summary"
)

// HistoryMessage 承载可展示的对话内容。
type HistoryMessage struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	TurnID     string         `json:"turnId,omitempty"`
	RequestID  string         `json:"requestId,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	Role       string         `json:"role"`
	AuthorKind string         `json:"authorKind"`
	Kind       MessageKind    `json:"kind"`
	Content    []MessageBlock `json:"content"`
	Timing     *Timing        `json:"timing,omitempty"`
}

type MessageBlock struct {
	Timing  *Timing `json:"timing,omitempty"`
	Kind    string  `json:"kind"`
	Text    string  `json:"text,omitempty"`
	CallID  string  `json:"callId,omitempty"`
	Name    string  `json:"name,omitempty"`
	Input   string  `json:"input,omitempty"`
	IsError bool    `json:"isError,omitzero"`
}

type Timing struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
}

type HistoryPage struct {
	Messages   []HistoryMessage       `json:"messages"`
	Requests   []ModelRequestActivity `json:"requests"`
	HasMore    bool                   `json:"hasMore"`
	NextCursor *int64                 `json:"nextCursor"`
}

// ModelRequestActivity 独立记录一次请求尝试，随历史查询或请求事件交付。
type ModelRequestActivity struct {
	RequestID   string              `json:"requestId"`
	RunID       string              `json:"runId"`
	TurnID      string              `json:"turnId,omitempty"`
	RequestKind string              `json:"requestKind"`
	RetryCount  int                 `json:"retryCount"`
	CreatedAt   time.Time           `json:"createdAt"`
	Timing      *Timing             `json:"timing,omitempty"`
	HistoryEnd  int64               `json:"historyEnd"`
	Result      *ModelRequestResult `json:"result,omitempty"`
}

type ModelRequestResult struct {
	Status     string      `json:"status"`
	Error      string      `json:"error,omitempty"`
	StopReason string      `json:"stopReason,omitempty"`
	Partial    string      `json:"partial,omitempty"`
	Usage      *ModelUsage `json:"usage,omitempty"`
}

type ModelUsage struct {
	InputTokens              *int64 `json:"inputTokens,omitempty"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

func publicMessages(values []*entry.Entry) []HistoryMessage {
	result := make([]HistoryMessage, 0, len(values))
	for _, value := range values {
		if message, ok := publicMessage(value); ok {
			result = append(result, message)
		}
	}
	return result
}

// publicMessage 是历史查询和事件推送共用的展示投影，不暴露内部推理续传状态。
func publicMessage(value *entry.Entry) (HistoryMessage, bool) {
	projected := HistoryMessage{
		ID:         value.ID.String(),
		RunID:      value.RunID.String(),
		TurnID:     value.TurnID.String(),
		CreatedAt:  value.CreatedAt,
		AuthorKind: string(value.AuthorKind),
	}
	switch payload := value.Payload.(type) {
	case entry.Message:
		projected.Kind = MessageChat
		if response := payload.Response; response != nil {
			projected.RequestID = response.RequestID.String()
			projected.Timing = (*Timing)(response.Timing)
		}
	case entry.Summary:
		projected.Kind = MessageSummary
		projected.Timing = (*Timing)(payload.Timing)
	default:
		return HistoryMessage{}, false
	}
	messageRole, blocks := value.Dialogue()
	projected.Role = string(messageRole)
	projected.Content = make([]MessageBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block := block.(type) {
		case entry.TextBlock:
			projected.Content = append(projected.Content, MessageBlock{
				Kind: "text",
				Text: block.Text,
			})
		case entry.ThinkingBlock:
			projected.Content = append(projected.Content, MessageBlock{
				Kind: "thinking",
				Text: block.Text,
			})
		case entry.ToolCallBlock:
			projected.Content = append(projected.Content, MessageBlock{
				Kind:   "tool_call",
				CallID: block.CallID,
				Name:   string(block.Name),
				Input:  block.Arguments,
			})
		case entry.ToolResultBlock:
			projected.Content = append(projected.Content, MessageBlock{
				Timing:  (*Timing)(block.Timing),
				Kind:    "tool_result",
				Text:    block.Text,
				CallID:  block.CallID,
				Name:    string(block.Name),
				IsError: block.IsError,
			})
		}
	}
	return projected, true
}

func publicHistoryPage(page sessionmodel.HistoryPage) HistoryPage {
	messages := make([]HistoryMessage, 0, len(page.Entries))
	requests := make([]ModelRequestActivity, 0)
	for i := range page.Entries {
		value := &page.Entries[i]
		request, ok := value.Payload.(entry.ModelRequest)
		if !ok {
			if message, ok := publicMessage(value); ok {
				messages = append(messages, message)
			}
			continue
		}
		activity := ModelRequestActivity{
			RequestID:   request.ID.String(),
			RunID:       value.RunID.String(),
			TurnID:      value.TurnID.String(),
			RequestKind: string(request.Kind),
			RetryCount:  request.RetryCount,
			CreatedAt:   value.CreatedAt,
			Timing:      (*Timing)(request.Timing),
			HistoryEnd:  value.HistoryEnd,
		}
		if request.Status != entry.RequestRunning {
			activity.Result = &ModelRequestResult{
				Status:     string(request.Status),
				StopReason: request.StopReason,
				Error:      request.Error,
			}
			if request.Status != entry.RequestCompleted {
				activity.Result.Partial = request.PartialText()
			}
			if request.Usage != nil && request.Usage.Reported() {
				activity.Result.Usage = (*ModelUsage)(request.Usage)
			}
		}
		requests = append(requests, activity)
	}
	return HistoryPage{
		Messages:   messages,
		Requests:   requests,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	}
}
