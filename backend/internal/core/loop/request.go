package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"strings"
	"time"
)

// summarizeContext 在真实模型请求边界执行摘要，不执行工具、不发布思考或回答文本。
// 请求和输出由 Execution Service 原生记录，原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	executionInput Input,
	history appcontext.ModelContext,
	limit int,
) (string, error) {
	sequence, err := r.Progress.NextRequestSequence(ctx, executionInput.ID)
	if err != nil {
		return "", failCause(FailureStorage, err)
	}
	turnID := identity.NewTurnID(executionInput.ID, sequence)
	r.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.TurnStarted,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
	})
	request := providerapi.Request{
		ExecutionID:     executionInput.ID,
		PromptCacheKey:  executionInput.SessionID.String(),
		Context:         history,
		Model:           executionInput.Config.Model,
		MaxOutputTokens: limit,
		TurnID:          turnID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := r.callModel(requestCtx, executionInput, request, hook.ModelRequestSummary, sequence, func(event providerapi.Event) {
		if event.Kind == providerapi.StreamUsage {
			r.emitModelEvent(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, event)
		}
	})
	if err != nil {
		return "", modelFailure(err)
	}
	if len(response.toolCalls()) != 0 {
		return "", errors.New("context summary returned a tool call")
	}
	var text strings.Builder
	for _, event := range response.events {
		if event.Kind == providerapi.StreamTextDelta {
			text.WriteString(event.Text)
		}
	}
	r.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.TurnCompleted,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
		StopReason:  response.stopReason,
	})
	return strings.TrimSpace(text.String()), nil
}

// buildModelRequest 在编排边界将任务快照映射为请求，模型调用只接收构造后的请求。
func buildModelRequest(
	request providerapi.Request,
	modelContext appcontext.ModelContext,
	catalog tool.ToolCatalog,
) providerapi.Request {
	request.Context = modelContext.Clone()
	request.Model = request.Model.Clone()
	request.Tools = catalog.List()
	return request
}
