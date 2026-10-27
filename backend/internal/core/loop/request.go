package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
	"strings"
	"time"
)

// summarizeContext 在真实模型请求边界执行摘要，不执行工具、不发布思考或回答文本。
// 请求和输出由 Execution Service 原生记录，原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	executionInput Input,
	deps ExecutionDependencies,
	history appcontext.ModelContext,
	limit int,
) (string, error) {
	sequence, err := deps.Execution.NextRequestSequence(ctx)
	if err != nil {
		return "", err
	}
	turnID := identity.NewTurnID(executionInput.ID, sequence)
	r.Emitter.Emit(coreevent.Event{
		Scope: coreevent.Scope{
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
		},
		Payload: coreevent.TurnStarted{},
	})
	request := providerapi.Request{
		ExecutionID:     executionInput.ID,
		PromptCacheKey:  executionInput.SessionID.String(),
		Context:         history,
		Model:           executionInput.Model,
		MaxOutputTokens: limit,
		TurnID:          turnID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := r.callModel(requestCtx, executionInput, deps, request, hook.ModelRequestSummary, sequence, func(event providerapi.Event) {
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
	r.emitTurnCompletion(executionInput, deps, turnID, nil, response.stopReason)
	return strings.TrimSpace(text.String()), nil
}
