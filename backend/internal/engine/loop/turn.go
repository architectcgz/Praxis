package loop

import (
	"context"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/modelcall"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/provider"
)

func (r *Runner) runTurn(
	ctx context.Context,
	params RunParams,
	deps RunDependencies,
	executor ToolExecutor,
	request provider.Request,
) (finished bool, err error) {
	turnID := request.TurnID
	iteration := hook.Iteration{Identity: runIdentity(params, turnID)}
	iteration.OperationID = identity.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeIteration(ctx, iteration); hookErr != nil {
		return false, errorcode.ErrInternal.Wrap(hookErr)
	}
	var stopReason provider.StopReason

	defer func() {
		commitCtx, cancel := r.commitContext(ctx)
		defer cancel()
		if hookErr := r.HookDispatcher.AfterIteration(commitCtx, hook.IterationResult{
			Iteration:  iteration,
			StopReason: stopReason,
			Err:        err,
		}); hookErr != nil {
			r.Logf("after iteration hook failed: %v", hookErr)
		}
	}()
	r.Logf("provider request prepared run_id=%s model=%s contextEntries=%d turn_id=%s", params.ID, params.Model.ModelID, len(request.Context.Entries), turnID)
	response, err := r.Request(ctx, params, deps, request, entry.ModelRequestIteration)
	stopReason = response.StopReason
	if err != nil {
		return false, modelcall.WrapModelError(err)
	}

	calls := prepareToolCalls(params, turnID, response.ToolCalls())
	commitCtx, cancel := r.commitContext(ctx)
	defer cancel()
	stored, inserted, err := deps.AppendEntry(commitCtx, entry.Entry{
		ID:         identity.NewEntryID(),
		RunID:      params.ID,
		TurnID:     turnID,
		AuthorKind: entry.AuthorAgent,
		Payload: entry.Message{
			Role:    entry.RoleAssistant,
			Content: response.Blocks(),
			Response: &entry.ModelResponse{
				RequestID:  response.RequestID,
				StopReason: string(stopReason),
				Timing:     response.Timing,
			},
		},
	})
	if err == nil {
		r.emit(params, turnID, event.EntriesAppended{Entries: []*entry.Entry{stored}})
	}
	if err != nil {
		return false, errorcode.ErrStorage.Wrap(err)
	}
	if inserted {
		committed := hook.EntryCommitted{Identity: runIdentity(params, turnID), Entry: *stored}
		if hookErr := r.HookDispatcher.AssistantCommitted(commitCtx, committed); hookErr != nil {
			r.Logf("assistant committed hook failed: %v", hookErr)
		}
		if len(calls) == 0 && (stopReason == provider.StopCompleted || stopReason == provider.StopSequence) {
			if hookErr := r.HookDispatcher.FinalOutputCommitted(commitCtx, committed); hookErr != nil {
				r.Logf("final output committed hook failed: %v", hookErr)
			}
		}
	}
	cancel()
	if len(calls) == 0 && (stopReason == provider.StopCompleted || stopReason == provider.StopSequence) {
		return true, nil
	}
	for _, call := range calls {
		r.emit(params, turnID, event.ToolCall{
			CallID:    call.CallID,
			Name:      string(call.Name),
			Arguments: call.Arguments,
		})
	}
	return false, r.runToolCalls(ctx, params, deps, executor, calls)
}
