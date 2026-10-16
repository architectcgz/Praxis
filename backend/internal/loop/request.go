package loop

import (
	"context"
	"errors"
	"strings"
	"time"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
	toolcontracts "praxis/internal/tools/contracts"
)

// summarizeContext 在真实模型请求边界执行摘要，不执行工具、不发布思考或回答文本。
// 请求在 Task 创建事务之外运行；原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	task taskmodel.Task,
	stream model.ModelStream,
	history appcontext.ModelContext,
	limit int,
	sequence uint64,
) (string, error) {
	if err := r.Progress.RecordSummaryRequest(ctx, task.ID, sequence); err != nil {
		return "", err
	}
	turnID := contracts.NewTurnID(task.ID, sequence)
	r.emit(Event{
		Kind:      EventTurnStarted,
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		TaskID:    task.ID,
		TurnID:    turnID,
	})
	request := model.ModelRequest{
		TaskID:           task.ID,
		SessionReference: task.SessionID.String(),
		Context:          history,
		Model:            task.Config.Model,
		MaxOutputTokens:  limit,
		TurnID:           turnID,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := requestModel(requestCtx, stream, request, hooks.Identity{
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		TaskID:    task.ID,
		TurnID:    turnID,
	}, r.HookDispatcher, func(event model.ModelStreamEvent) {
		if event.Kind == model.StreamUsage {
			r.emitModelEvent(task.SessionID, task.AgentID, task.ID, turnID, event)
		}
	})
	if err != nil {
		return "", modelFailure(err)
	}
	if err := r.Progress.RecordModelOutput(ctx, task.ID, response.textBytes); err != nil {
		return "", failCause(contracts.TaskFailureStorage, err)
	}
	if len(response.toolCalls()) != 0 {
		return "", errors.New("context summary returned a tool call")
	}
	var text strings.Builder
	for _, event := range response.events {
		if event.Kind == model.StreamTextDelta {
			text.WriteString(event.Text)
		}
	}
	r.emit(Event{
		Kind:       EventTurnCompleted,
		SessionID:  task.SessionID,
		AgentID:    task.AgentID,
		TaskID:     task.ID,
		TurnID:     turnID,
		StopReason: response.stopReason,
	})
	return strings.TrimSpace(text.String()), nil
}

// buildModelRequest 在编排边界将任务快照映射为请求，模型调用只接收构造后的请求。
func buildModelRequest(
	request model.ModelRequest,
	modelContext appcontext.ModelContext,
	catalog toolcontracts.ToolCatalog,
) model.ModelRequest {
	request.Context = modelContext.Clone()
	request.Model = request.Model.Clone()
	request.Tools = catalog.List()
	return request
}
