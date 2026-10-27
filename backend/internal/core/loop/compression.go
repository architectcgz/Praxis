package loop

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
)

// compressContext 在模型请求边界编排压缩、摘要追加和 Hook。
// 算法只接收生成摘要的模型请求能力，不依赖 Execution 服务或历史存储。
func (r *Runner) compressContext(
	ctx context.Context,
	input Input,
	deps ExecutionDependencies,
	value appcontext.ModelContext,
	tools []ToolDefinition,
) (compressed appcontext.ModelContext, err error) {
	toolPayload, err := json.Marshal(tools)
	if err != nil {
		return value, NewFailure(FailureContract, err)
	}
	event := hook.ContextCompression{
		Identity: executionIdentity(input, ""),
		Context:  value,
	}
	event.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeContextCompression(ctx, event); hookErr != nil {
		return value, NewFailure(FailureInternal, hookErr)
	}
	changed := false
	defer func() {
		if err != nil {
			compressed = value
			changed = false
		}
		if hookErr := r.HookDispatcher.AfterContextCompression(context.WithoutCancel(ctx), hook.ContextCompressionResult{
			ContextCompression: event,
			ContextAfter:       compressed,
			Changed:            changed,
			Err:                err,
		}); hookErr != nil {
			r.Logf("after context compression hook failed: %v", hookErr)
		}
	}()
	compressed, changed, err = contextcompress.Compress(ctx, value, contextcompress.Options{
		SummaryFormat:   r.SummaryFormat,
		ContextWindow:   input.Model.ContextWindow,
		MaxOutputTokens: input.Model.MaxOutputTokens,
		ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
	}, func(summaryCtx context.Context, history appcontext.ModelContext, limit int) (string, error) {
		return r.summarizeContext(summaryCtx, input, deps, history, limit)
	})
	if err != nil || !changed {
		return compressed, err
	}
	// 摘要先追加到原始历史，再将包含摘要和保留条目的完整结果替换进内存上下文。
	var appended []sessionentry.ContextEntry
	if len(compressed.Entries) > 0 && compressed.Entries[0].IsSummary() && compressed.Entries[0].ID == "" {
		entry := compressed.Entries[0]
		entry.AuthorID, entry.ExecutionID, entry.RequestID = input.AgentID.String(), input.ID.String(), input.RequestID.String()
		encoded, encodeErr := json.Marshal(struct {
			Content  []sessionentry.Block
			Replaces []string
		}{
			Content:  entry.Content,
			Replaces: entry.Replaces,
		})
		if encodeErr != nil {
			return value, NewFailure(FailureContract, encodeErr)
		}
		digest := sha256.Sum256(encoded)
		entry.ID = fmt.Sprintf("summary:%s:%x", input.ID, digest[:12])
		stored, inserted, writeErr := deps.History.AppendEntry(ctx, entry)
		if writeErr != nil {
			return value, writeErr
		}
		compressed.Entries[0] = stored
		if inserted {
			appended = []sessionentry.ContextEntry{stored}
		}
	}
	if replaceErr := deps.History.ReplaceContext(context.WithoutCancel(ctx), compressed.Entries); replaceErr != nil {
		return value, replaceErr
	}
	deps.History.PublishEntries("", appended)
	r.Logf("context compressed execution_id=%s entriesBefore=%d entriesAfter=%d", input.ID, len(value.Entries), len(compressed.Entries))
	return compressed, nil
}
