package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// PrepareContext 在模型请求前执行容量准备；压缩策略、摘要提交和替代关系归执行层管理。
// summarize 只提供真实模型调用能力，失败时不向 Loop 返回残缺的压缩上下文。
func (e *loopExecution) PrepareContext(
	ctx context.Context,
	value appcontext.ModelContext,
	tools []loop.ToolDefinition,
	summarize contextcompress.SummarizeFunc,
) (prepared appcontext.ModelContext, err error) {
	toolPayload, err := json.Marshal(tools)
	if err != nil {
		return appcontext.ModelContext{}, loop.NewFailure(loop.FailureContract, err)
	}
	event := hook.ContextCompression{
		Identity: e.identity(""),
		Context:  value,
	}
	event.OperationID = hook.NewOperationID()
	if hookErr := e.service.hookDispatcher.BeforeContextCompression(ctx, event); hookErr != nil {
		return appcontext.ModelContext{}, loop.NewFailure(loop.FailureInternal, hookErr)
	}
	changed := false
	defer func() {
		if err != nil {
			prepared = value
			changed = false
		}
		if hookErr := e.service.hookDispatcher.AfterContextCompression(context.WithoutCancel(ctx), hook.ContextCompressionResult{
			ContextCompression: event,
			ContextAfter:       prepared,
			Changed:            changed,
			Err:                err,
		}); hookErr != nil {
			e.service.logger.Errorf("after context compression hook failed: %v", hookErr)
		}
	}()
	task := e.run.Snapshot()
	prepared, changed, err = e.service.compressionStrategy.Compress(ctx, value, contextcompress.Options{
		SummaryFormat:   e.service.summaryFormat,
		ContextWindow:   task.Config.Model.ContextWindow,
		MaxOutputTokens: task.Config.Model.MaxOutputTokens,
		ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
	}, summarize)
	if err != nil || !changed {
		return prepared, err
	}
	for index, entry := range prepared.Entries {
		if !entry.IsSummary() || entry.ID != "" {
			continue
		}
		entry.Role, entry.AuthorKind = role.User, sessionentry.AuthorSystem
		entry.AuthorID, entry.ExecutionID, entry.RequestID = task.AgentID.String(), task.ID.String(), task.RequestID.String()
		encoded, encodeErr := json.Marshal(struct {
			Content  []sessionentry.Block
			Replaces []string
		}{
			Content:  entry.Content,
			Replaces: entry.Replaces,
		})
		if encodeErr != nil {
			return prepared, loop.NewFailure(loop.FailureContract, encodeErr)
		}
		digest := sha256.Sum256(encoded)
		entry.ID = fmt.Sprintf("summary:%s:%x", task.ID, digest[:12])
		stored, inserted, writeErr := e.AppendEntry(ctx, entry)
		if writeErr != nil {
			return prepared, writeErr
		}
		prepared.Entries[index] = stored
		if inserted {
			e.PublishEntries("", []sessionentry.ContextEntry{stored})
		}
	}
	e.service.logger.Infof("context compressed execution_id=%s entriesBefore=%d entriesAfter=%d", task.ID, len(value.Entries), len(prepared.Entries))
	return prepared, nil
}
