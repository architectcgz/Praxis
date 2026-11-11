package loop

import (
	"context"
	"errors"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
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
	recorded := false
	defer func() {
		if recorded {
			r.emit(params, turnID, coreevent.TurnCompleted{StopReason: stopReason})
		}
		if hookErr := r.HookDispatcher.AfterIteration(context.WithoutCancel(ctx), hook.IterationResult{
			Iteration:  iteration,
			StopReason: stopReason,
			Err:        err,
		}); hookErr != nil {
			r.Logf("after iteration hook failed: %v", hookErr)
		}
	}()
	r.emit(params, turnID, coreevent.TurnStarted{})
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", params.ID, params.Model.ModelID, len(request.Context.Entries), turnID)
	response, err := r.Request(ctx, params, deps, request, hook.ModelRequestIteration)
	stopReason = response.StopReason
	blocks := response.Blocks()
	entry := assistantEntry(params, request, stopReason, blocks)
	entry.Response.Timing = response.Timing
	if err != nil {
		partial := make([]sessionentry.ContentBlock, 0, len(blocks))
		for _, block := range blocks {
			switch block.(type) {
			case sessionentry.TextBlock, sessionentry.ThinkingBlock:
				partial = append(partial, block)
			}
		}
		stopReason = "error"
		if ctx.Err() != nil {
			stopReason = "aborted"
		}
		entry.Content = partial
		entry.Response.StopReason, entry.Response.Error = string(stopReason), err.Error()
		err = NewModelFailure(err)
		accepted, _, writeErr := deps.AppendEntry(context.WithoutCancel(ctx), entry)
		recorded = accepted != nil
		if writeErr != nil {
			err = NewFailure(FailureStorage, errors.Join(err, writeErr))
		}
		return false, err
	}

	calls := prepareToolCalls(params, turnID, response.ToolCalls())
	accepted, _, err := deps.AppendEntry(ctx, entry)
	recorded = accepted != nil
	if err != nil {
		return false, err
	}
	if stopReason == providerapi.StopCompleted || stopReason == providerapi.StopSequence {
		return true, nil
	}
	for _, call := range calls {
		r.emit(params, turnID, coreevent.ToolCall{
			CallID:    call.CallID,
			Name:      string(call.Name),
			Arguments: call.Arguments,
		})
	}
	completed, err := r.runToolCalls(ctx, params, deps.AppendEntry, executor, calls)
	if err != nil {
		// 已取得真实回执的调用即使保存失败也不改写；只收尾尚未取得回执的调用。
		if closeErr := interruptToolCalls(ctx, deps.AppendEntry, calls[completed:]); closeErr != nil {
			err = NewFailure(FailureStorage, errors.Join(err, closeErr))
		}
	}
	return false, err
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
