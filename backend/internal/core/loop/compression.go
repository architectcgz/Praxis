package loop

import (
	"context"
	"encoding/json"
	"fmt"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	sessionentry "praxis/internal/core/entry"
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/timing"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"slices"
)

// Compress 在上下文构建完成后独立评估容量，按需生成摘要并通过统一入口提交 Session。
func (r *Runner) Compress(ctx context.Context, params RunParams, deps RunDependencies, input *sessionentry.Entry, value appcontext.ModelContext) (compressed appcontext.ModelContext, err error) {
	reservedTokens, err := estimateRequestOverhead(r.Tools.List(), nil)
	if err != nil {
		return value, NewFailure(FailureContract, err)
	}
	event := hook.ContextCompression{
		Identity: runIdentity(params, ""),
		Context:  value,
	}
	event.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeContextCompression(ctx, event); hookErr != nil {
		return value, NewFailure(FailureInternal, hookErr)
	}
	compressed, changed := value, false
	defer func() {
		if hookErr := r.HookDispatcher.AfterContextCompression(context.WithoutCancel(ctx), hook.ContextCompressionResult{
			ContextCompression: event,
			ContextAfter:       compressed,
			Changed:            changed,
			Err:                err,
		}); hookErr != nil {
			r.Logf("after context compression hook failed: %v", hookErr)
		}
	}()
	request := providerapi.Request{
		Context:         value,
		Model:           params.Model,
		Tools:           r.Tools.List(),
		MaxOutputTokens: params.Model.MaxOutputTokens,
	}
	inputTokens, _, err := requestTokens(request, deps.Budget)
	if err != nil {
		return value, NewFailure(FailureContract, err)
	}
	span := timing.NewSpan()
	span.Start()
	var requests []identity.ModelRequestID
	var turnID identity.TurnID
	generated, needed, err := contextcompress.Compress(ctx, value, input, contextcompress.Options{
		InputTokens:     inputTokens,
		ContextWindow:   params.Model.ContextWindow,
		MaxOutputTokens: params.Model.MaxOutputTokens,
		ReservedTokens:  reservedTokens + appcontext.Margin(params.Model.ContextWindow) + 128,
	}, func(summaryCtx context.Context, summaryInput appcontext.ModelContext, limit int) (string, error) {
		text, requestID, requestTurn, err := r.summarizeContext(summaryCtx, params, deps, summaryInput, limit)
		turnID = requestTurn
		if requestID != "" {
			requests = append(requests, requestID)
		}
		return text, err
	})
	measured := span.End()
	if err != nil {
		return value, NewModelFailure(err)
	}
	if !needed {
		return value, nil
	}
	entry := generated.Entries[0]
	summary := entry.Payload.(entryPayload.Summary)
	summary.Timing = measured
	summary.RequestIDs = requests
	entry.Payload = summary
	entry.RunID = params.ID.String()
	entry.ID = "summary:" + requests[len(requests)-1].String()
	entry.TurnID = turnID.String()
	accepted, _, err := deps.AppendEntry(context.WithoutCancel(ctx), *entry)
	if err != nil {
		return value, err
	}
	compressed, changed = generated, true
	compressed.Entries[0] = accepted
	r.emit(params, "", coreevent.EntriesAppended{Entries: []sessionentry.Entry{*accepted}})

	r.Logf("context compressed run_id=%s entriesBefore=%d entriesAfter=%d", params.ID, len(value.Entries), len(compressed.Entries))
	return compressed, nil
}

func requestShape(request providerapi.Request) (string, int, error) {
	data, err := json.Marshal(struct {
		Model  any
		Tools  []tool.ToolDefinition
		Schema *providerapi.OutputSchema
	}{
		Model:  request.Model,
		Tools:  request.Tools,
		Schema: request.OutputSchema,
	})
	if err != nil {
		return "", 0, err
	}
	overhead, err := estimateRequestOverhead(request.Tools, request.OutputSchema)
	return string(data), overhead, err
}

func requestTokens(request providerapi.Request, budget *appcontext.Budget) (int, string, error) {
	key, overhead, err := requestShape(request)
	if err != nil {
		return 0, "", err
	}
	if budget != nil {
		tokens, err := budget.Estimate(request.Context, key, overhead)
		return tokens, key, err
	}
	tokens, err := request.Context.EstimatedTokens()
	return tokens + overhead + 128, key, err
}

func remindContextBudget(request providerapi.Request, budget *appcontext.Budget) (providerapi.Request, error) {
	tokens, _, err := requestTokens(request, budget)
	if err != nil {
		return request, err
	}
	if appcontext.NearContextLimit(tokens, request.MaxOutputTokens, request.Model.ContextWindow) {
		hint := &sessionentry.Entry{
			Payload: entryPayload.Message{
				Role: role.User,
				Content: []sessionentry.ContentBlock{
					sessionentry.TextBlock{Text: fmt.Sprintf("[本次请求的运行提示] 估算输入 %d tokens，输出预留 %d tokens，窗口 %d tokens。请优先完成当前任务，并按需分段读取工具输出。", tokens, request.MaxOutputTokens, request.Model.ContextWindow)},
				},
			},
		}
		request.Context.Entries = append(slices.Clone(request.Context.Entries), hint)
		tokens, _, err = requestTokens(request, budget)
		if err != nil {
			return request, err
		}
	}
	return request, appcontext.CheckCapacity(tokens, request.MaxOutputTokens, request.Model.ContextWindow)
}

func estimateRequestOverhead(tools []tool.ToolDefinition, schema *providerapi.OutputSchema) (int, error) {
	data, err := json.Marshal(struct {
		Tools        []tool.ToolDefinition
		OutputSchema *providerapi.OutputSchema
	}{
		Tools:        tools,
		OutputSchema: schema,
	})
	if err != nil {
		return 0, err
	}
	return appcontext.EstimateTokens(string(data)), nil
}
