package agentruntime

import (
	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	appcontext "praxis/internal/core/context"
)

// ModelStreamEventKind identifies an event emitted by a model provider adapter.
type ModelStreamEventKind string

const (
	StreamTextDelta     ModelStreamEventKind = "text_delta"
	StreamThinkingDelta ModelStreamEventKind = "thinking_delta"
	StreamToolCall      ModelStreamEventKind = "tool_call"
	StreamUsage         ModelStreamEventKind = "usage"
	StreamComplete      ModelStreamEventKind = "complete"
	StreamError         ModelStreamEventKind = "error"
)

// ModelUsage 记录一次模型请求的累计 token 用量；InputTokens 包含缓存读取和写入部分。
// 可选计数为 nil 表示 Provider 未提供该值，不能当作零用量。
type ModelUsage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// Valid 校验 Provider 返回的计数；无效计数不应影响模型响应，只是不参与统计。
func (u ModelUsage) Valid() bool {
	if u.InputTokens < 0 {
		return false
	}
	if u.OutputTokens != nil && (*u.OutputTokens < 0 || *u.OutputTokens > math.MaxInt64-u.InputTokens) {
		return false
	}
	var read, creation int64
	if u.CacheReadInputTokens != nil {
		read = *u.CacheReadInputTokens
	}
	if u.CacheCreationInputTokens != nil {
		creation = *u.CacheCreationInputTokens
	}
	return read >= 0 && creation >= 0 && creation <= u.InputTokens && read <= u.InputTokens-creation
}

// ModelUsageRecord 按 Turn 和 step 保存请求用量，同一请求的累计更新不重复计费。
type ModelUsageRecord struct {
	SessionID string     `json:"sessionId"`
	AgentID   string     `json:"agentId"`
	TurnID    string     `json:"turnId"`
	Step      int        `json:"step"`
	Usage     ModelUsage `json:"usage"`
}

// Key 返回请求的稳定身份，供持久化和实时更新去重。
func (r ModelUsageRecord) Key() string {
	return fmt.Sprintf("usage:%s:%d", r.TurnID, r.Step)
}

// Validate 拒绝非规范化身份和无效计数，不修正持久化数据。
func (r ModelUsageRecord) Validate() error {
	for _, id := range []string{r.SessionID, r.AgentID, r.TurnID} {
		if id == "" || id != strings.TrimSpace(id) {
			return errors.New("token 用量身份必须是规范化非空字符串")
		}
	}
	if r.Step < 1 || !r.Usage.Valid() {
		return errors.New("token 用量的 step 或计数无效")
	}
	return nil
}

// ModelStreamEvent 是单次模型请求的 Provider 中立流事件。
type ModelStreamEvent struct {
	Kind       ModelStreamEventKind
	Text       string
	ToolCall   toolcontracts.ToolCall
	Usage      *ModelUsage
	StopReason string
	Err        error
}

// ModelRequest 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type ModelRequest struct {
	TurnID           contracts.TurnID
	SessionReference string
	Context          appcontext.ModelContext
	Model            contracts.ModelSnapshot
	MaxOutputTokens  int
	Tools            []toolcontracts.ToolDefinition
	Step             int
}

// TurnModel 根据冻结模型配置解析 Provider 流和输出预算，凭据只在运行时读取。
//
// 模型的上下文窗口不在这里：窗口只用于本地提前拒绝，所有权归 provider adapter
// （它才知道本协议序列化出的请求体）。
type TurnModel struct {
	Stream          ModelStream
	MaxOutputTokens int
}

// ModelStream 定义 Agent runtime 消费的统一模型流接口。
// Provider 适配器实现此接口，不向运行时暴露协议细节。
type ModelStream interface {
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
