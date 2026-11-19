package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"slices"
	"sync"
	"time"
)

func prepareToolCalls(params RunParams, turnID identity.TurnID, values []providerapi.ToolCall) []sessionentry.ToolCall {
	calls := make([]sessionentry.ToolCall, 0, len(values))
	for _, call := range values {
		calls = append(calls, sessionentry.ToolCall{
			Key: sessionentry.Key{
				RunID:  params.ID,
				CallID: call.ID,
			},
			SessionID: params.SessionID,
			AgentID:   params.AgentID,
			TurnID:    turnID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return calls
}

// runToolCalls 用只读批次和修改屏障执行，完成顺序只影响通知，全部回执按声明顺序一次保存。
func (r *Runner) runToolCalls(ctx context.Context, params RunParams, deps RunDependencies, executor ToolExecutor, calls []sessionentry.ToolCall) error {
	if len(calls) == 0 {
		return nil
	}
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()
	outcomes := make([]toolexecution.CallResult, len(calls))
	failures := make([]error, len(calls))
	completed := make([]bool, len(calls))
	saveRequest := func(ctx context.Context, request sessionentry.ModelRequest) error {
		return r.saveModelRequest(ctx, params, deps.AppendEntry, request)
	}
	var executionErr error
	deps.ExecuteTools(saveRequest, func(submitRequest toolexecution.SaveModelRequest) {
		execute := func(index int) {
			call := calls[index]
			outcomes[index], failures[index] = executor.Execute(batchCtx, call, submitRequest)
			if failures[index] != nil {
				cancelBatch()
			}
			completed[index] = true
			result := outcomes[index].ToolResult
			if failures[index] != nil && result.Payload == "" {
				result = tool.NewToolError("tool_execution_failed", failures[index].Error())
				outcomes[index].ToolResult = result
			}
			r.emit(params, call.TurnID, coreevent.ToolResult{
				CallID:  call.CallID,
				Result:  result.Payload,
				IsError: result.ErrorClass != "",
				Timing:  outcomes[index].Timing,
			})
		}
		for index := 0; index < len(calls); {
			if err := ctx.Err(); err != nil {
				executionErr = err
				break
			}
			end := index + 1
			if tool.IsReadOnly(r.Tools, calls[index].Name) {
				for end < len(calls) && tool.IsReadOnly(r.Tools, calls[end].Name) {
					end++
				}
				var group sync.WaitGroup
				for current := index; current < end; current++ {
					group.Go(func() { execute(current) })
				}
				group.Wait()
			} else {
				execute(index)
			}
			for _, err := range failures[index:end] {
				executionErr = errors.Join(executionErr, err)
			}
			if executionErr != nil {
				break
			}
			index = end
		}
	})
	for index := range calls {
		if !completed[index] {
			outcomes[index].ToolResult = tool.NewToolError("tool_interrupted", "The execution exited before a reliable tool result was available.")
		}
	}
	if err := r.boundToolResults(ctx, params, deps, executor, outcomes); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	content := make([]sessionentry.ContentBlock, len(calls))
	for index, call := range calls {
		result := outcomes[index]
		content[index] = sessionentry.ToolResultBlock{
			CallID:  call.CallID,
			Name:    call.Name,
			Text:    result.ToolResult.Payload,
			IsError: result.ToolResult.ErrorClass != "",
			Timing:  result.Timing,
		}
	}
	value := sessionentry.Entry{
		ID:         "tools:" + calls[0].TurnID.String(),
		RunID:      params.ID.String(),
		TurnID:     calls[0].TurnID.String(),
		AuthorKind: sessionentry.AuthorTool,
		Payload: sessionentry.Message{
			Role:    role.Tool,
			Content: content,
		},
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	stored, inserted, err := deps.AppendEntry(commitCtx, value)
	if err != nil {
		return NewFailure(FailureStorage, errors.Join(executionErr, err))
	}
	r.emit(params, calls[0].TurnID, coreevent.EntriesAppended{Entries: []sessionentry.Entry{*stored}})
	if inserted {
		for index, result := range outcomes {
			if result.OperationID == "" {
				continue
			}
			identity := runIdentity(params, calls[index].TurnID)
			identity.OperationID = result.OperationID
			if err := r.HookDispatcher.AfterToolExecution(context.WithoutCancel(ctx), hook.ToolExecutionResult{ToolExecution: hook.ToolExecution{Identity: identity, Call: calls[index], Arguments: string(result.Arguments)}, Result: result.ToolResult, Err: result.Err}); err != nil {
				r.Logf("after tool execution hook failed: %v", err)
			}
		}
	}
	return executionErr
}

// boundToolResults 在正式回执创建前共享整组预算，优先转存最大的结果，不截断结构化内容。
func (r *Runner) boundToolResults(ctx context.Context, params RunParams, deps RunDependencies, executor ToolExecutor, results []toolexecution.CallResult) error {
	limiter, ok := executor.(interface {
		BoundResult(tool.ToolResult, int) (tool.ToolResult, error)
	})
	if !ok {
		return nil
	}
	entries, err := deps.ReadEntries(context.WithoutCancel(ctx))
	if err != nil {
		return NewFailure(FailureStorage, err)
	}
	request := providerapi.Request{
		Context:         r.BuildContext(ctx, params, entries),
		Model:           params.Model,
		Tools:           r.Tools.List(),
		MaxOutputTokens: params.Model.MaxOutputTokens,
	}
	input, _, err := requestTokens(request, deps.Budget)
	if err != nil {
		return err
	}
	budget := max(0, params.Model.ContextWindow-params.Model.MaxOutputTokens-appcontext.Margin(params.Model.ContextWindow)-input)
	if params.Model.ContextWindow <= 0 {
		return nil
	}
	sizes := make([]int, len(results))
	order := make([]int, len(results))
	total := 0
	for i, result := range results {
		sizes[i] = appcontext.EstimateTokens(result.ToolResult.Payload) + 32
		total += sizes[i]
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return sizes[b] - sizes[a] })
	for _, index := range order {
		if total <= budget {
			break
		}
		if sizes[index] <= 256 {
			continue
		}
		prior := results[index].ToolResult
		bounded, err := limiter.BoundResult(prior, 0)
		if err != nil {
			bounded = tool.NewToolError("tool_output_save_failed", "The operation already ran, but saving its output failed: "+err.Error()+". Do not rerun the operation to recover output.")
			bounded.SideEffect = prior.SideEffect
		}
		results[index].ToolResult = bounded
		total -= sizes[index]
		sizes[index] = appcontext.EstimateTokens(bounded.Payload) + 32
		total += sizes[index]
	}
	return nil
}
