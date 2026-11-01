package execution

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// Execute 直接编排模型回合、Assistant 记录、逐个工具执行和回执记录。
// 已有最终回答时只允许轮后摘要，不再请求模型执行用户任务。
func (s *Service) Execute(ctx context.Context, run *agentexecution.Run, runner *loop.Runner, executor loop.ToolExecutor) (err error) {
	var turnID identity.TurnID
	defer func() {
		if err != nil {
			s.emitError(run, turnID, err)
		}
	}()
	if !run.Snapshot().Active() {
		return loop.NewFailure(loop.FailureContract, errorcode.ErrRequestConflict)
	}
	execution, deps := s.modelExecutionParams(run)
	if run.Snapshot().Progress.FinalAssistantID == "" {
		for {
			content, contextErr := s.contextContent(ctx, run)
			if contextErr != nil {
				return modelFailure(contextErr)
			}
			modelContext, contextErr := runner.BuildContext(ctx, execution, appcontext.ModelContext{
				SystemPrompt: content.SystemPrompt,
				Entries:      content.Entries,
			})
			if contextErr != nil {
				return modelFailure(contextErr)
			}
			sequence, sequenceErr := deps.Execution.NextRequestSequence(ctx)
			if sequenceErr != nil {
				return sequenceErr
			}
			turnID = identity.NewTurnID(execution.ID, sequence)
			request := providerapi.Request{
				ExecutionID:     execution.ID,
				PromptCacheKey:  execution.SessionID.String(),
				Model:           execution.Model.Clone(),
				Context:         modelContext,
				Tools:           runner.Tools.List(),
				MaxOutputTokens: execution.Model.MaxOutputTokens,
				TurnID:          turnID,
			}
			finished, turnErr := s.executeTurn(ctx, run, runner, executor, execution, deps, request, sequence)
			if turnErr != nil {
				return turnErr
			}
			if finished {
				break
			}
		}
	}
	if compressionErr := s.Compact(ctx, run, runner); compressionErr != nil {
		s.logger.Errorf("context compression failed execution=%s; completed answer preserved: %v", execution.ID, compressionErr)
	}
	return nil
}

func (s *Service) executeTurn(
	ctx context.Context,
	run *agentexecution.Run,
	runner *loop.Runner,
	executor loop.ToolExecutor,
	execution loop.ExecutionParams,
	deps loop.ExecutionDependencies,
	request providerapi.Request,
	sequence uint64,
) (finished bool, err error) {
	turnID := request.TurnID
	iteration := hook.Iteration{Identity: executionIdentity(run, turnID), Sequence: sequence}
	iteration.OperationID = hook.NewOperationID()
	if hookErr := s.hookDispatcher.BeforeIteration(ctx, iteration); hookErr != nil {
		return false, loop.NewFailure(loop.FailureInternal, hookErr)
	}
	var stopReason providerapi.StopReason
	recorded := false
	defer func() {
		if recorded {
			s.emitTurnCompletion(run, turnID, stopReason)
		}
		if hookErr := s.hookDispatcher.AfterIteration(context.WithoutCancel(ctx), hook.IterationResult{
			Iteration:  iteration,
			StopReason: stopReason,
			Err:        err,
		}); hookErr != nil {
			s.logger.Errorf("after iteration hook failed: %v", hookErr)
		}
	}()
	s.emitter.Emit(coreevent.Event{Source: coreevent.SourceForRun(run, turnID), Payload: coreevent.TurnStarted{}})
	s.logger.Infof("provider request prepared id=%s model=%s contextEntries=%d turn=%s", execution.ID, execution.Model.ModelID, len(request.Context.Entries), turnID)
	s.emitter.Emit(coreevent.Event{Source: coreevent.SourceForRun(run, turnID), Payload: coreevent.ProviderWaiting{}})
	response, err := runner.Request(ctx, execution, deps, request, hook.ModelRequestIteration, sequence)
	stopReason = response.StopReason
	blocks := response.Blocks()
	if err != nil {
		partial := make([]sessionentry.Block, 0, len(blocks))
		for _, block := range blocks {
			if block.Kind == sessionentry.BlockText || block.Kind == sessionentry.BlockThinking {
				partial = append(partial, block)
			}
		}
		err = modelFailure(err)
		if len(partial) > 0 {
			stopReason = "error"
			if ctx.Err() != nil {
				stopReason = "aborted"
			}
			accepted, _, writeErr := s.appendEntry(context.WithoutCancel(ctx), run, assistantEntry(execution, turnID, stopReason, partial))
			recorded = accepted.ID != ""
			if writeErr != nil {
				err = entryFailure(run, errors.Join(err, writeErr))
			}
		}
		return false, err
	}

	calls := prepareToolCalls(execution, turnID, response.ToolCalls())
	accepted, _, err := s.appendEntry(ctx, run, assistantEntry(execution, turnID, stopReason, blocks))
	recorded = accepted.ID != ""
	if err != nil {
		return false, err
	}
	if stopReason == providerapi.StopCompleted || stopReason == providerapi.StopSequence {
		return true, nil
	}
	for _, call := range calls {
		s.emitter.Emit(coreevent.Event{
			Source: coreevent.SourceForRun(run, turnID),
			Payload: coreevent.ToolCall{
				CallID: call.CallID,
				Name:   string(call.Name),
				Input:  call.Arguments,
			},
		})
	}
	completed, err := s.runToolCalls(ctx, run, executor, calls)
	if err != nil {
		// 已取得真实回执的调用即使保存失败也不改写；只收尾尚未取得回执的调用。
		if closeErr := s.interruptToolCalls(ctx, run, calls[completed:]); closeErr != nil {
			err = entryFailure(run, errors.Join(err, closeErr))
		}
	}
	return false, err
}

func assistantEntry(execution loop.ExecutionParams, turnID identity.TurnID, stopReason providerapi.StopReason, blocks []sessionentry.Block) sessionentry.ContextEntry {
	return sessionentry.ContextEntry{
		ID:          "assistant:" + turnID.String(),
		RequestID:   execution.RequestID.String(),
		ExecutionID: execution.ID.String(),
		TurnID:      turnID.String(),
		StopReason:  string(stopReason),
		Role:        role.Assistant,
		AuthorKind:  sessionentry.AuthorAgent,
		AuthorID:    execution.AgentID.String(),
		Content:     blocks,
	}
}
