package loop

import (
	"errors"
	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	taskmodel "praxis/internal/core/task"
)

func (r *Runner) emit(event agentruntime.AgentEvent) {
	r.EventObserver(event)
}

func (r *Runner) emitToolResult(
	task taskmodel.Task,
	turnID contracts.TurnID,
	call ToolCall,
	block appcontext.ContextBlock,
) {
	r.emit(agentruntime.AgentEvent{
		Kind:    agentruntime.AgentEventToolResult,
		AgentID: task.AgentID,
		TaskID:  task.ID,
		TurnID:  turnID,
		CallID:  call.ID,
		Name:    string(call.Name),
		Result:  block.Text,
		IsError: block.IsError,
	})
}

func (r *Runner) emitError(task taskmodel.Task, turnID contracts.TurnID, err error) {
	if err == nil {
		return
	}
	message := "task failed"
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr.Message != "" {
		message = runtimeErr.Message
	} else {
		var failure *taskFailure
		if errors.As(err, &failure) {
			if detail := contracts.TaskFailureMessage(failure.code, failure.cause); detail != "" {
				message = detail
			}
		}
	}
	r.emit(agentruntime.AgentEvent{
		Kind:    agentruntime.AgentEventError,
		AgentID: task.AgentID,
		TaskID:  task.ID,
		TurnID:  turnID,
		Error:   message,
	})
}
