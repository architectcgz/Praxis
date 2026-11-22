package loop

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/modelcall"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
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
	iteration.OperationID = identity.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeIteration(ctx, iteration); hookErr != nil {
		return false, errorcode.ErrInternal.Wrap(hookErr)
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
	r.Logf("provider request prepared run_id=%s model=%s contextEntries=%d turn_id=%s", params.ID, params.Model.ModelID, len(request.Context.Entries), turnID)
	response, err := r.Request(ctx, params, deps, request, sessionentry.ModelRequestIteration)
	stopReason = response.StopReason
	if err != nil {
		return false, modelcall.WrapModelError(err)
	}

	calls := prepareToolCalls(params, turnID, response.ToolCalls())
	stored, inserted, err := deps.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
		ID:         identity.NewEntryID(),
		RunID:      params.ID,
		TurnID:     turnID,
		AuthorKind: sessionentry.AuthorAgent,
		Payload: sessionentry.Message{
			Role:    sessionentry.RoleAssistant,
			Content: response.Blocks(),
			Response: &sessionentry.ModelResponse{
				RequestID:  response.RequestID,
				StopReason: string(stopReason),
				Timing:     response.Timing,
			},
		},
	})
	if err == nil {
		r.emit(params, turnID, coreevent.EntriesAppended{Entries: []*sessionentry.Entry{stored}})
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
