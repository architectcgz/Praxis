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

// compressContext 在完整用户轮次结束后编排全量摘要、提交和 Hook。
// 算法只接收生成摘要的模型请求能力，不依赖 Execution 服务或会话存储。
func (r *Runner) compressContext(
	ctx context.Context,
	input Input,
	deps ExecutionDependencies,
	value appcontext.ModelContext,
	tools []ToolDefinition,
) (compressed appcontext.ModelContext, err error) {
	reservedTokens, err := estimateRequestOverhead(tools, nil)
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
		ReservedTokens:  reservedTokens,
	}, func(summaryCtx context.Context, summaryInput appcontext.ModelContext, limit int) (string, error) {
		return r.summarizeContext(summaryCtx, input, deps, summaryInput, limit)
	})
	if err != nil || !changed {
		return compressed, err
	}
	// 摘要覆盖此前整个 Context，成功提交后 Run 才切换为摘要引用。
	var appended []sessionentry.ContextEntry
	if len(compressed.Entries) > 0 && compressed.Entries[0].IsSummary() && compressed.Entries[0].ID == "" {
		entry := compressed.Entries[0]
		entry.AuthorID, entry.ExecutionID, entry.RequestID = input.AgentID.String(), input.ID.String(), input.RequestID.String()
		encoded, encodeErr := json.Marshal(entry.Content)
		if encodeErr != nil {
			return value, NewFailure(FailureContract, encodeErr)
		}
		digest := sha256.Sum256(encoded)
		entry.ID = fmt.Sprintf("summary:%s:%x", input.ID, digest[:12])
		stored, inserted, writeErr := deps.Entries.AppendEntry(ctx, entry)
		if writeErr != nil {
			return value, writeErr
		}
		compressed.Entries[0] = stored
		if inserted {
			appended = []sessionentry.ContextEntry{stored}
		}
	}
	deps.Entries.PublishEntries("", appended)
	r.Logf("context compressed execution_id=%s entriesBefore=%d entriesAfter=%d", input.ID, len(value.Entries), len(compressed.Entries))
	return compressed, nil
}
