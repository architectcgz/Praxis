package model

import (
	"bytes"
	"context"
	"strings"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	toolcontracts "praxis/internal/tools/contracts"
)

// ModelStreamEventKind 标识模型 Provider 适配器产生的流事件类型。
type ModelStreamEventKind string

const (
	StreamTextDelta     ModelStreamEventKind = "text_delta"
	StreamThinkingDelta ModelStreamEventKind = "thinking_delta"
	StreamToolCall      ModelStreamEventKind = "tool_call"
	StreamUsage         ModelStreamEventKind = "usage"
	StreamComplete      ModelStreamEventKind = "complete"
	StreamError         ModelStreamEventKind = "error"
)

// ModelStreamEvent 是单次模型请求的 Provider 中立流事件。
type ModelStreamEvent struct {
	Kind       ModelStreamEventKind
	Text       string
	ToolCall   toolcontracts.ToolCall
	Usage      *ModelUsage
	StopReason string
	Err        error
}

// ContextBlocksFromEvents 把模型流事件转换为有序上下文 block。
// events 来自流消费阶段收集的文本、思考和工具调用事件；工具参数会复制，调用身份不再规范化。
// 空白文本不进入上下文，但模型输出预算仍由流消费阶段按原始文本统计。
func ContextBlocksFromEvents(events []ModelStreamEvent) []appcontext.ContextBlock {
	blocks := make([]appcontext.ContextBlock, 0, len(events))
	for _, event := range events {
		if event.Kind == StreamToolCall {
			call := event.ToolCall
			blocks = append(blocks, appcontext.ContextBlock{
				Kind:   appcontext.ContextBlockToolCall,
				CallID: call.ID,
				Name:   string(call.Name),
				Input:  bytes.Clone(call.Arguments),
			})
			continue
		}
		if strings.TrimSpace(event.Text) == "" {
			continue
		}
		kind := appcontext.ContextBlockText
		if event.Kind == StreamThinkingDelta {
			kind = appcontext.ContextBlockThinking
		}
		blocks = append(blocks, appcontext.ContextBlock{Kind: kind, Text: event.Text})
	}
	return blocks
}

// ModelRequest 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type ModelRequest struct {
	TaskID           contracts.TaskID
	SessionReference string
	Context          appcontext.ModelContext
	Model            ModelSnapshot
	MaxOutputTokens  int
	Tools            []toolcontracts.ToolDefinition
	TurnID           contracts.TurnID
}

// Model 根据冻结模型配置提供模型流和输出预算，凭据只在运行时读取。
// 上下文压缩使用请求中的冻结 ModelSnapshot；最终编码请求的窗口校验归 Provider adapter。
type Model struct {
	Stream          ModelStream
	MaxOutputTokens int
}

// ModelBuilder 按已冻结的模型快照构建本次执行的模型。
type ModelBuilder interface {
	// BuildModel 返回模型流和输出预算；快照无效或 Provider 无法构建时返回错误。
	BuildModel(ModelSnapshot) (Model, error)
}

// ModelStream 定义模型调用方消费的统一模型流接口。
// Provider 适配器实现此接口，不向调用方暴露协议细节。
type ModelStream interface {
	// Stream 发起模型请求并返回事件通道；请求无效或连接失败时返回错误。
	// 调用方提前停止消费时必须取消 context，以释放 Provider 流资源。
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
