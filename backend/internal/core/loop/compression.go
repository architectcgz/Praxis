package loop

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// compressContext 包围策略检查与摘要保存；Changed 区分无需压缩和完整完成压缩。
// 是否压缩、保护范围与摘要保存仍由原生策略和业务提交能力负责。
func (r *Runner) compressContext(
	ctx context.Context,
	input Input,
	history appcontext.ModelContext,
	options contextcompress.Options,
) (compressed appcontext.ModelContext, changed bool, err error) {
	event := hook.ContextCompression{Identity: executionIdentity(input, ""), Context: history}
	event.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeContextCompression(ctx, event); hookErr != nil {
		return compressed, false, failCause(FailureInternal, hookErr)
	}
	defer func() {
		if err != nil {
			compressed = history
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
	compressed, changed, err = r.CompressionStrategy.Compress(ctx, history, options, func(summaryCtx context.Context, value appcontext.ModelContext, limit int) (string, error) {
		return r.summarizeContext(summaryCtx, input, value, limit)
	})
	if err != nil || !changed {
		return compressed, changed, err
	}
	for index, entry := range compressed.Entries {
		if !entry.IsSummary() || entry.ID != "" {
			continue
		}
		entry.Role, entry.AuthorKind = role.User, sessionentry.AuthorSystem
		entry.AuthorID, entry.ExecutionID, entry.RequestID = input.AgentID.String(), input.ID.String(), input.RequestID.String()
		encoded, encodeErr := json.Marshal(struct {
			Content  []sessionentry.Block
			Replaces []string
		}{Content: entry.Content, Replaces: entry.Replaces})
		if encodeErr != nil {
			return compressed, false, failCause(FailureContract, encodeErr)
		}
		digest := sha256.Sum256(encoded)
		entry.ID = fmt.Sprintf("summary:%s:%x", input.ID, digest[:12])
		stored, inserted, writeErr := r.appendEntry(ctx, input, entry)
		if writeErr != nil {
			return compressed, false, entryFailure(input, writeErr)
		}
		compressed.Entries[index] = stored
		if !inserted {
			continue
		}
		r.Emitter.Emit(coreevent.Event{
			Transient:   !input.Runtime.Main(),
			Kind:        coreevent.EntriesAppended,
			SessionID:   input.SessionID,
			AgentID:     input.AgentID,
			ExecutionID: input.ID,
			Entries:     []sessionentry.ContextEntry{stored},
		})
	}
	return compressed, changed, nil
}
