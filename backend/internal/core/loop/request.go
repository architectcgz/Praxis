package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"strings"
	"time"
)

// summarizeContext 在真实模型请求边界执行摘要，不执行工具、不发布思考或回答文本。
// 请求在 执行输入 创建事务之外运行；原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	input Input,
	stream modelapi.ModelStream,
	history appcontext.ModelContext,
	limit int,
	sequence uint64,
) (string, error) {
	if err := input.Progress.RecordSummaryRequest(ctx, sequence); err != nil {
		return "", err
	}
	turnID := identity.NewTurnID(input.ID, sequence)
	r.emit(Event{
		Kind:        EventTurnStarted,
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
	})
	request := modelapi.ModelRequest{
		ExecutionID:     input.ID,
		PromptCacheKey:  input.SessionID.String(),
		Context:         history,
		Model:           input.Config.Model,
		MaxOutputTokens: limit,
		TurnID:          turnID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := requestModel(requestCtx, stream, request, hook.Identity{
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
	}, r.HookDispatcher, func(event modelapi.ModelStreamEvent) {
		if event.Kind == modelapi.StreamUsage {
			r.emitModelEvent(input.SessionID, input.AgentID, input.ID, turnID, event)
		}
	})
	if err != nil {
		return "", modelFailure(err)
	}
	if err := input.Progress.RecordModelOutput(ctx, response.textBytes); err != nil {
		return "", failCause(FailureStorage, err)
	}
	if len(response.toolCalls()) != 0 {
		return "", errors.New("context summary returned a tool call")
	}
	var text strings.Builder
	for _, event := range response.events {
		if event.Kind == modelapi.StreamTextDelta {
			text.WriteString(event.Text)
		}
	}
	r.emit(Event{
		Kind:        EventTurnCompleted,
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
		StopReason:  response.stopReason,
	})
	return strings.TrimSpace(text.String()), nil
}

// buildModelRequest 在编排边界将任务快照映射为请求，模型调用只接收构造后的请求。
func buildModelRequest(
	request modelapi.ModelRequest,
	modelContext appcontext.ModelContext,
	catalog tool.ToolCatalog,
) modelapi.ModelRequest {
	request.Context = modelContext.Clone()
	request.Model = request.Model.Clone()
	request.Tools = catalog.List()
	return request
}
