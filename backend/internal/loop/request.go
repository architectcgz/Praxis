package loop

import (
	"context"
	"errors"
	"strings"
	"time"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	toolcontracts "praxis/internal/tools/contracts"
)

// summarizeContext 通过 TurnExecutor 执行摘要请求，不执行工具、不发布思考或回答文本。
// 请求在 Task 创建事务之外运行；原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	task taskmodel.Task,
	stream model.ModelStream,
	history appcontext.ModelContext,
	limit int,
	sequence uint64,
	budget *taskBudget,
) (string, error) {
	var response modelResponse
	var text strings.Builder
	outcome, code, err := r.Turns.Execute(ctx, contracts.TurnExecutionContext{
		TaskID:    task.ID,
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		Sequence:  sequence,
	}, func(summaryCtx context.Context, turnID contracts.TurnID) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
		r.emit(agentruntime.AgentEvent{
			Kind:      agentruntime.AgentEventTurnStarted,
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			TaskID:    task.ID,
			TurnID:    turnID,
		})
		request := model.ModelRequest{
			TaskID:           task.ID,
			SessionReference: task.SessionID.String(),
			Context:          history,
			Model:            task.Input.Model,
			MaxOutputTokens:  limit,
			TurnID:           turnID,
		}
		if err := budget.reserveInput(request); err != nil {
			r.emitError(task.AgentID, task.ID, turnID, err)
			return taskResult(err)
		}
		requestContext, cancel := context.WithTimeout(summaryCtx, 2*time.Minute)
		defer cancel()
		r.Logf("context summary requested task=%s turn=%s entries=%d", task.ID, turnID, len(history.Entries))
		var requestErr error
		response, requestErr = requestModel(requestContext, stream, request, func(event model.ModelStreamEvent) {
			if event.Kind == model.StreamUsage {
				r.emitModelEvent(task.SessionID, task.AgentID, task.ID, turnID, event)
			}
		})
		if requestErr == nil {
			requestErr = budget.addOutput(response.textBytes)
		}
		if requestErr != nil {
			requestErr = modelFailure(requestErr)
			r.emitError(task.AgentID, task.ID, turnID, requestErr)
			return taskResult(requestErr)
		}
		if len(response.toolCalls()) != 0 {
			err := errors.New("context summary returned a tool call")
			r.emitError(task.AgentID, task.ID, turnID, err)
			return taskResult(err)
		}
		for _, event := range response.events {
			if event.Kind == model.StreamTextDelta {
				text.WriteString(event.Text)
			}
		}
		r.emit(agentruntime.AgentEvent{
			Kind:       agentruntime.AgentEventTurnCompleted,
			SessionID:  task.SessionID,
			AgentID:    task.AgentID,
			TaskID:     task.ID,
			TurnID:     turnID,
			StopReason: response.stopReason,
		})
		return taskmodel.TaskCompleted, "", nil
	})
	if err != nil || outcome != taskmodel.TaskCompleted {
		return "", failCause(code, err)
	}
	return text.String(), nil
}

// buildModelRequest 在编排边界将任务快照映射为请求，模型调用只接收构造后的请求。
func buildModelRequest(
	request model.ModelRequest,
	modelContext appcontext.ModelContext,
	permissions contracts.TaskPermissions,
	catalog toolcontracts.ToolCatalog,
) model.ModelRequest {
	request.Context = modelContext.Clone()
	request.Model = request.Model.Clone()
	request.Tools = allowedToolDefinitions(permissions, catalog)
	return request
}

// allowedToolDefinitions 只筛选模型可见工具；具体调用的执行授权由工具调用服务负责。
func allowedToolDefinitions(permissions contracts.TaskPermissions, catalog toolcontracts.ToolCatalog) []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(permissions.AllowedTools))
	for _, definition := range catalog.List() {
		if permissions.AllowsTool(definition.Name) {
			definitions = append(definitions, definition.Snapshot())
		}
		r.emit(agentruntime.AgentEvent{
			Kind:      agentruntime.AgentEventProviderWaiting,
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			TaskID:    task.ID,
			TurnID:    turnID,
			Name:      task.Input.Model.ProviderID + "/" + task.Input.Model.ModelID,
		})
	}
	return definitions
}
