package loop

import (
	"context"
	"errors"
	"fmt"
	appcontext "praxis/internal/context"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/modelcall"
	sessionentry "praxis/internal/entry"
	"praxis/internal/errorcode"
	coreevent "praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/provider"
	"praxis/internal/timing"
	"slices"
	"strings"
	"time"
)

// Compress 在上下文构建完成后独立评估容量，按需生成摘要并通过统一入口提交 Session。
func (r *Runner) Compress(ctx context.Context, params RunParams, deps RunDependencies, input *sessionentry.Entry, value appcontext.ModelContext) (compressed appcontext.ModelContext, err error) {
	reservedTokens, err := modelcall.EstimateOverhead(params.Tools.List(), nil)
	if err != nil {
		return value, errorcode.ErrContract.Wrap(err)
	}
	event := hook.ContextCompression{
		Identity: runIdentity(params, ""),
		Context:  value,
	}
	event.OperationID = identity.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeContextCompression(ctx, event); hookErr != nil {
		return value, errorcode.ErrInternal.Wrap(hookErr)
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
	request := provider.Request{
		Context:         value,
		Model:           params.Model,
		Tools:           params.Tools.List(),
		MaxOutputTokens: params.Model.MaxOutputTokens,
	}
	inputTokens, _, err := modelcall.EstimateRequestTokens(request, deps.Budget)
	if err != nil {
		return value, errorcode.ErrContract.Wrap(err)
	}
	span := timing.NewSpan()
	span.Start()
	var requests []identity.ModelRequestID
	generated, needed, err := appcontext.Compress(ctx, value, input, appcontext.CompressOptions{
		InputTokens:     inputTokens,
		ContextWindow:   params.Model.ContextWindow,
		MaxOutputTokens: params.Model.MaxOutputTokens,
		ReservedTokens:  reservedTokens + appcontext.Margin(params.Model.ContextWindow) + 128,
	}, func(summaryCtx context.Context, summaryInput appcontext.ModelContext, limit int) (string, error) {
		text, requestID, err := r.summarizeContext(summaryCtx, params, deps, summaryInput, limit)
		if requestID != "" {
			requests = append(requests, requestID)
		}
		return text, err
	})
	measured := span.End()
	if err != nil {
		return value, modelcall.WrapModelError(err)
	}
	if !needed {
		return value, nil
	}
	entry := generated.Entries[0]
	summary := entry.Payload.(sessionentry.Summary)
	summary.Timing = measured
	summary.RequestIDs = requests
	entry.Payload = summary
	entry.ID = identity.NewEntryID()
	entry.RunID = params.ID
	accepted, _, err := deps.AppendEntry(context.WithoutCancel(ctx), *entry)
	if err != nil {
		return value, err
	}
	compressed, changed = generated, true
	compressed.Entries[0] = accepted
	r.emit(params, "", coreevent.EntriesAppended{Entries: []*sessionentry.Entry{accepted}})

	r.Logf("context compressed run_id=%s entriesBefore=%d entriesAfter=%d", params.ID, len(value.Entries), len(compressed.Entries))
	return compressed, nil
}

func remindContextBudget(request provider.Request, budget *appcontext.Budget) (provider.Request, error) {
	tokens, _, err := modelcall.EstimateRequestTokens(request, budget)
	if err != nil {
		return request, err
	}
	if appcontext.NearContextLimit(tokens, request.MaxOutputTokens, request.Model.ContextWindow) {
		hint := &sessionentry.Entry{
			Payload: sessionentry.Message{
				Role: sessionentry.RoleUser,
				Content: []sessionentry.ContentBlock{
					sessionentry.TextBlock{Text: fmt.Sprintf("[本次请求的运行提示] 估算输入 %d tokens，输出预留 %d tokens，窗口 %d tokens。请优先完成当前任务，并按需分段读取工具输出。", tokens, request.MaxOutputTokens, request.Model.ContextWindow)},
				},
			},
		}
		request.Context.Entries = append(slices.Clone(request.Context.Entries), hint)
		tokens, _, err = modelcall.EstimateRequestTokens(request, budget)
		if err != nil {
			return request, err
		}
	}
	return request, appcontext.CheckCapacity(tokens, request.MaxOutputTokens, request.Model.ContextWindow)
}

// summarizeContext 在 Turn 之间生成归属于 Run 的摘要，不执行工具或发布回答文本。
func (r *Runner) summarizeContext(ctx context.Context, params RunParams, deps RunDependencies, input appcontext.ModelContext, limit int) (string, identity.ModelRequestID, error) {
	request := provider.Request{
		RunID:           params.ID,
		PromptCacheKey:  params.SessionID.String(),
		Context:         input,
		Model:           params.Model,
		MaxOutputTokens: limit,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := r.Request(requestCtx, params, deps, request, sessionentry.ModelRequestSummary)
	if err != nil {
		return "", response.RequestID, err
	}
	if len(response.ToolCalls()) != 0 {
		return "", response.RequestID, errors.New("context summary returned a tool call")
	}
	return strings.TrimSpace(response.Text()), response.RequestID, nil
}
