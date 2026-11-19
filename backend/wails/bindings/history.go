package bindings

import (
	"praxis/internal/core/entry"
	entryPayload "praxis/internal/core/entry/payload"
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
	Error      string         `json:"error,omitempty"`
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

// publicMessages 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicMessages(values []entry.Entry) []HistoryMessage {
	result := make([]HistoryMessage, 0, len(values))
	for _, value := range values {
		projected := HistoryMessage{
			ID:         value.ID,
			RunID:      value.RunID,
			TurnID:     value.TurnID,
			CreatedAt:  value.CreatedAt,
			AuthorKind: string(value.AuthorKind),
		}
		switch payload := value.Payload.(type) {
		case entryPayload.Message:
			projected.Kind = MessageChat
			if response := payload.Response; response != nil {
				projected.RequestID = response.RequestID.String()
				projected.Error = response.Error
				projected.Timing = (*Timing)(response.Timing)
			}
		case entryPayload.Summary:
			projected.Kind = MessageSummary
			projected.Timing = (*Timing)(payload.Timing)
		default:
			continue
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
		result = append(result, projected)
	}
	return result
}

func publicHistoryPage(page sessionmodel.HistoryPage) HistoryPage {
	requests := make([]ModelRequestActivity, 0)
	for _, value := range page.Entries {
		request, ok := value.Payload.(entryPayload.ModelRequest)
		if !ok {
			continue
		}
		activity := ModelRequestActivity{
			RequestID:   request.ID.String(),
			RunID:       value.RunID,
			TurnID:      value.TurnID,
			RequestKind: string(request.Kind),
			RetryCount:  request.RetryCount,
			CreatedAt:   value.CreatedAt,
			Timing:      (*Timing)(request.Timing),
			HistoryEnd:  value.HistoryEnd,
		}
		if request.Status != entryPayload.RequestRunning {
			activity.Result = &ModelRequestResult{
				Status:     string(request.Status),
				StopReason: request.StopReason,
				Error:      request.Error,
			}
			if request.Status != entryPayload.RequestCompleted {
				activity.Result.Partial = request.PartialText()
			}
			if request.Usage != nil && request.Usage.Reported() {
				activity.Result.Usage = (*ModelUsage)(request.Usage)
			}
		}
		requests = append(requests, activity)
	}
	return HistoryPage{
		Messages:   publicMessages(page.Entries),
		Requests:   requests,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	}
}
