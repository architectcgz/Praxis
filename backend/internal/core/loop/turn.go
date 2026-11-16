package loop

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
)

func (r *Runner) runTurn(
	ctx context.Context,
	params RunParams,
	deps RunDependencies,
	executor ToolExecutor,
	request providerapi.Request,
) (finished bool, err error) {
	turnID := request.TurnID
	iteration := hook.Iteration{Identity: runIdentity(params, turnID)}
	iteration.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeIteration(ctx, iteration); hookErr != nil {
		return false, NewFailure(FailureInternal, hookErr)
	}
	var stopReason providerapi.StopReason

	defer func() {
		if hookErr := r.HookDispatcher.AfterIteration(context.WithoutCancel(ctx), hook.IterationResult{
			Iteration:  iteration,
			StopReason: stopReason,
			Err:        err,
		}); hookErr != nil {
			r.Logf("after iteration hook failed: %v", hookErr)
		}
	}()
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", params.ID, params.Model.ModelID, len(request.Context.Entries), turnID)
	response, err := r.Request(ctx, params, deps, request, hook.ModelRequestIteration)
	stopReason = response.StopReason
	request.ID = response.RequestID
	blocks := response.Blocks()
	entry := assistantEntry(params, request, stopReason, blocks)
	entry.Response.Timing = response.Timing
	if err != nil {
		return false, NewModelFailure(err)
	}

	calls := prepareToolCalls(params, turnID, response.ToolCalls())
	stored, inserted, err := deps.AppendEntry(context.WithoutCancel(ctx), entry)
	if err == nil {
		r.emit(params, turnID, coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{*stored}})
	}
	if err != nil {
		return false, err
	}
	if inserted {
		committed := hook.EntryCommitted{Identity: runIdentity(params, turnID), Entry: *stored}
		if hookErr := r.HookDispatcher.AssistantCommitted(context.WithoutCancel(ctx), committed); hookErr != nil {
			r.Logf("assistant committed hook failed: %v", hookErr)
		}
		if len(calls) == 0 && (stopReason == providerapi.StopCompleted || stopReason == providerapi.StopSequence) {
			if hookErr := r.HookDispatcher.FinalOutputCommitted(context.WithoutCancel(ctx), committed); hookErr != nil {
				r.Logf("final output committed hook failed: %v", hookErr)
			}
		}
	}
	if len(calls) == 0 && (stopReason == providerapi.StopCompleted || stopReason == providerapi.StopSequence) {
		return true, nil
	}
	for _, call := range calls {
		r.emit(params, turnID, coreevent.ToolCall{
			CallID:    call.CallID,
			Name:      string(call.Name),
			Arguments: call.Arguments,
		})
	}
	return false, r.runToolCalls(ctx, params, deps, executor, calls)
}

func assistantEntry(params RunParams, request providerapi.Request, stopReason providerapi.StopReason, blocks []sessionentry.ContentBlock) sessionentry.ContextEntry {
	return sessionentry.ContextEntry{
		ID:     "assistant:" + request.ID.String(),
		RunID:  params.ID.String(),
		TurnID: request.TurnID.String(),
		Response: &sessionentry.ModelResponse{
			RequestID:  request.ID,
			StopReason: string(stopReason),
		},
		Role:       role.Assistant,
		AuthorKind: sessionentry.AuthorAgent,
		AuthorID:   params.AgentID.String(),
		Content:    blocks,
	}
}
