package loop

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"strings"
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
	r.Logf("provider request prepared run_id=%s model=%s contextEntries=%d turn_id=%s", params.ID, params.Model.ModelID, len(request.Context.Entries), turnID)
	response, err := r.Request(ctx, params, deps, request, sessionentry.ModelRequestIteration)
	stopReason = response.StopReason
	if err != nil {
		return false, NewModelFailure(err)
	}

	calls := prepareToolCalls(params, turnID, response.ToolCalls())
	stored, inserted, err := deps.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
		ID:         "assistant:" + response.RequestID.String(),
		RunID:      params.ID.String(),
		TurnID:     turnID.String(),
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

// entryBlocksFromEvents 把模型流事件转换为有序上下文 block。
// events 来自流消费阶段收集的有序内容和推理状态；参数保留原文，推理状态载荷深拷贝。
// 空白文本不进入上下文，但模型输出预算仍由流消费阶段按原始文本统计。
func entryBlocksFromEvents(events []providerapi.Event) []sessionentry.ContentBlock {
	blocks := make([]sessionentry.ContentBlock, 0, len(events))
	for _, event := range events {
		if event.Kind == providerapi.StreamReasoningState {
			blocks = append(blocks, sessionentry.ReasoningStateBlock{
				State: event.ReasoningState.Clone(),
			})
			continue
		}
		if event.Kind == providerapi.StreamToolCall {
			call := event.ToolCall
			blocks = append(blocks, sessionentry.ToolCallBlock{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
			continue
		}
		if (event.Kind != providerapi.StreamTextDelta && event.Kind != providerapi.StreamThinkingDelta) || strings.TrimSpace(event.Text) == "" {
			continue
		}
		if event.Kind == providerapi.StreamThinkingDelta {
			blocks = append(blocks, sessionentry.ThinkingBlock{Text: event.Text})
		} else {
			blocks = append(blocks, sessionentry.TextBlock{Text: event.Text})
		}
	}
	return blocks
}
