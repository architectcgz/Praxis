package entry

import (
	"context"
	"encoding/json"
	"praxis/internal/core/entry/role"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"strings"
	"time"
)

type AuthorKind string

const (
	AuthorUser   AuthorKind = "user"
	AuthorAgent  AuthorKind = "agent"
	AuthorTool   AuthorKind = "tool"
	AuthorSystem AuthorKind = "system"
)

// ModelResponse 只描述产生本条文本、思考或工具声明的模型响应。
type ModelResponse struct {
	RequestID  identity.ModelRequestID `json:"requestId"`
	StopReason string                  `json:"stopReason,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Timing     *timing.Record          `json:"timing,omitempty"`
}

// Compression 描述 Run 级摘要及仍需保留的原文，不属于压缩前后任一 Turn。
// 分片摘要可能包含多个模型请求；保留条目通过 ID 关联 Session 中的同一份 Entry。
type Compression struct {
	RequestIDs []identity.ModelRequestID `json:"requestIds"`
	Timing     *timing.Record            `json:"timing,omitempty"`
}

// Entry 是会话记录与模型上下文共用的内部有序条目，不定义前端协议。
type Entry struct {
	HistoryEnd  int64          `json:"-"` // 记录的结束位置，由 History 提供。
	Payload     Payload        `json:"-"`
	ID          string         `json:"id"`
	RunID       string         `json:"runId,omitempty"`
	TurnID      string         `json:"turnId,omitempty"`
	Response    *ModelResponse `json:"response,omitempty"`
	Compression *Compression   `json:"compression,omitempty"`
	AuthorKind  AuthorKind     `json:"authorKind,omitempty"`
	AuthorID    string         `json:"authorId,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`

	Role    role.Role      `json:"role"`
	Content []ContentBlock `json:"content"`
}

// IsSummary 识别摘要事实；上下文选择按最近摘要及后续历史进行。
func (e Entry) IsSummary() bool {
	return e.Compression != nil
}

// UnmarshalJSON 在持久化读取入口按 kind 解码内容，内部直接使用具体类型。
func (e *Entry) UnmarshalJSON(data []byte) error {
	type entry Entry
	decoded := struct {
		*entry
		RawContent []json.RawMessage `json:"content"`
		RawPayload json.RawMessage   `json:"payload"`
	}{entry: (*entry)(e)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var err error
	e.Payload, err = decodePayload(decoded.RawPayload)
	if err != nil {
		return err
	}
	e.Content = make([]ContentBlock, len(decoded.RawContent))
	for index, raw := range decoded.RawContent {
		block, err := decodeContentBlock(raw)
		if err != nil {
			return err
		}
		e.Content[index] = block
	}
	if p, ok := e.Payload.(UserInput); ok {
		e.Role = role.User
		e.AuthorKind = AuthorUser
		e.Content = []ContentBlock{TextBlock{Text: p.Text}}
	}
	return nil
}

// TextContent 返回正文块，保留完整历史中的其他内容供各消费方选择。
func (e Entry) TextContent() string {
	var text strings.Builder
	for _, block := range e.Content {
		if value, ok := block.(TextBlock); ok {
			text.WriteString(value.Text)
		}
	}
	return text.String()
}

// Preview 提供历史目录的短文本，不保存第二份正文。
func (e Entry) Preview() string {
	if e.IsRun() {
		return "执行结束 · " + e.ExecutionResult().Outcome
	}
	if e.IsSummary() {
		return "上下文摘要"
	}
	text := strings.Join(strings.Fields(e.TextContent()), " ")
	if text == "" {
		text = string(e.Role)
	}
	runes := []rune(text)
	if len(runes) > 100 {
		text = string(runes[:100]) + "…"
	}
	return text
}

// Values 为请求或展示边界创建值列表，内部载荷仍按只读数据共享。
func Values(entries []*Entry) []Entry {
	values := make([]Entry, len(entries))
	for index, entry := range entries {
		values[index] = *entry
	}
	return values
}

func (e Entry) MarshalJSON() ([]byte, error) {
	type value Entry
	payload, err := encodePayload(e.Payload)
	if err != nil {
		return nil, err
	}
	if _, ok := e.Payload.(UserInput); ok {
		e.Content = nil
	}
	return json.Marshal(struct {
		value
		Payload json.RawMessage `json:"payload,omitempty"`
	}{
		value:   value(e),
		Payload: payload,
	})
}

// ContributesToModelContext 判断 Entry 是否会进入下一次模型请求。
func (e Entry) ContributesToModelContext() bool {
	if e.IsSummary() {
		return true
	}
	if e.IsRun() {
		return false
	}
	if request, _ := e.RequestFact(); request != nil {
		return false
	}
	switch e.Payload.(type) {
	case UserInput, SubagentReportAccepted, SubagentInstructionAccepted:
		return true
	}
	return e.Role == role.User || e.Role == role.Assistant || e.Role == role.Tool
}

// AppendEntry 是执行流程唯一的同步事实提交能力，归属在准备时绑定。
type AppendEntry func(context.Context, Entry) (*Entry, bool, error)
