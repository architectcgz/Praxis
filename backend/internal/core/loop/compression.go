package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
)

// Compress 在上下文构建完成后独立评估容量，按需生成摘要并通过统一入口提交 Session。
func (r *Runner) Compress(ctx context.Context, params RunParams, deps RunDependencies, input *sessionentry.ContextEntry, value appcontext.ModelContext) (compressed appcontext.ModelContext, err error) {
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
	entry.Compression.Timing = measured
	entry.Compression.RequestIDs = requests
	entry.AuthorID, entry.RunID = params.AgentID.String(), params.ID.String()
	entry.ID = "summary:" + requests[len(requests)-1].String()
	entry.TurnID = turnID.String()
	accepted, _, err := deps.AppendEntry(context.WithoutCancel(ctx), *entry)
	if err != nil {
		return value, err
	}
	compressed, changed = generated, true
	compressed.Entries[0] = accepted
	r.emit(params, "", coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{*accepted}})

	r.Logf("context compressed run_id=%s entriesBefore=%d entriesAfter=%d", params.ID, len(value.Entries), len(compressed.Entries))
	return compressed, nil
}
