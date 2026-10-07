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
	toolcontracts "praxis/internal/tools/contracts"
)

// summarizeContext 独立记录摘要调用的 Turn 和用量，不执行工具、不发布思考或回答文本。
// 请求在 Task 创建事务之外运行；失败时结算摘要 Turn，原历史仍由调用方持有。
func (r *Runner) summarizeContext(
	ctx context.Context,
	task taskmodel.Task,
	stream model.ModelStream,
	history appcontext.ModelContext,
	limit int,
	sequence uint64,
	budget *taskBudget,
) (string, error) {
	turn, err := r.Turns.RecordStart(ctx, TurnParams{
		TaskID:    task.ID,
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		Sequence:  sequence,
	})
	if err != nil {
		return "", failCause(contracts.TaskFailureStorage, err)
	}
	request := model.ModelRequest{
		TaskID:           task.ID,
		SessionReference: task.SessionID.String(),
		Context:          history,
		Model:            task.Input.Model,
		MaxOutputTokens:  limit,
		TurnID:           turn.ID,
	}
	var response modelResponse
	if err = budget.reserveInput(request); err == nil {
		summaryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		r.Logf("context summary requested task=%s turn=%s entries=%d", task.ID, turn.ID, len(history.Entries))
		response, err = requestModel(summaryCtx, stream, request, func(event model.ModelStreamEvent) {
			if event.Kind == model.StreamUsage {
				r.emitModelEvent(task.SessionID, task.AgentID, task.ID, turn.ID, event)
			}
		})
		if err == nil {
			err = budget.addOutput(response.textBytes)
		}
	}
	var text strings.Builder
	if err == nil {
		if len(response.toolCalls()) != 0 {
			err = errors.New("context summary returned a tool call")
		} else {
			for _, event := range response.events {
				if event.Kind == model.StreamTextDelta {
					text.WriteString(event.Text)
				}
			}
		}
	}
	if err != nil {
		_, code, settled := r.settleTurnErr(ctx, turn.ID, modelFailure(err))
		return "", failCause(code, settled)
	}
	_, _, err = r.settleTurn(ctx, turn.ID, taskmodel.TaskCompleted, "", nil)
	if err != nil {
		return "", failCause(contracts.TaskFailureStorage, err)
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
	}
	return definitions
}
