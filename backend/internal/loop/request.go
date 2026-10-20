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
// 请求和输出由同步 Hook 记录，原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	executionInput Input,
	stream modelapi.ModelStream,
	history appcontext.ModelContext,
	limit int,
) (string, error) {
	sequence, err := r.Requests.NextRequestSequence(ctx, executionInput.ID)
	if err != nil {
		return "", failCause(FailureStorage, err)
	}
	turnID := identity.NewTurnID(executionInput.ID, sequence)
	r.emit(Event{
		Kind:        EventTurnStarted,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
	})
	request := modelapi.ModelRequest{
		ExecutionID:     executionInput.ID,
		PromptCacheKey:  executionInput.SessionID.String(),
		Context:         history,
		Model:           executionInput.Config.Model,
		MaxOutputTokens: limit,
		TurnID:          turnID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := requestModel(requestCtx, stream, request, hook.ModelRequest{
		Identity: hook.Identity{
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
		},
		Kind:     hook.ModelRequestSummary,
		Sequence: sequence,
	}, r.HookDispatcher, func(event modelapi.ModelStreamEvent) {
		if event.Kind == modelapi.StreamUsage {
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
		if event.Kind == modelapi.StreamTextDelta {
			text.WriteString(event.Text)
		}
	}
	r.emit(Event{
		Kind:        EventTurnCompleted,
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
	request modelapi.ModelRequest,
	modelContext appcontext.ModelContext,
	catalog tool.ToolCatalog,
) modelapi.ModelRequest {
	request.Context = modelContext.Clone()
	request.Model = request.Model.Clone()
	request.Tools = catalog.List()
	return request
}
