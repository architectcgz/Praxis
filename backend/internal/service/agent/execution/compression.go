package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
)

// Compact 只在完整用户轮次结束后生成并记录摘要，使用与其他 Entry 相同的接纳顺序。
func (s *Service) Compact(ctx context.Context, run *agentexecution.Run, runner *loop.Runner) (err error) {
	content, err := s.contextContent(ctx, run)
	if err != nil {
		return err
	}
	execution, deps := s.modelExecutionParams(run)
	value := appcontext.ModelContext{
		SystemPrompt: content.SystemPrompt,
		Entries:      content.Entries,
	}
	event := hook.ContextCompression{
		Identity: executionIdentity(run, ""),
		Context:  value,
	}
	event.OperationID = hook.NewOperationID()
	if hookErr := s.hookDispatcher.BeforeContextCompression(ctx, event); hookErr != nil {
		return loop.NewFailure(loop.FailureInternal, hookErr)
	}
	compressed, changed := value, false
	defer func() {
		if hookErr := s.hookDispatcher.AfterContextCompression(context.WithoutCancel(ctx), hook.ContextCompressionResult{
			ContextCompression: event,
			ContextAfter:       compressed,
			Changed:            changed,
			Err:                err,
		}); hookErr != nil {
			s.logger.Errorf("after context compression hook failed: %v", hookErr)
		}
	}()
	generated, needed, err := runner.Compress(ctx, execution, deps, value)
	if err != nil {
		return modelFailure(err)
	}
	if !needed {
		return nil
	}
	entry := generated.Entries[0]
	entry.AuthorID, entry.ExecutionID, entry.RequestID = execution.AgentID.String(), execution.ID.String(), execution.RequestID.String()
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return loop.NewFailure(loop.FailureContract, err)
	}
	digest := sha256.Sum256(encoded)
	entry.ID = fmt.Sprintf("summary:%s:%x", execution.ID, digest[:12])
	accepted, _, err := s.appendEntry(ctx, run, entry)
	if accepted.ID != "" {
		compressed, changed = generated, true
		compressed.Entries[0] = accepted
	}
	if err != nil {
		return err
	}
	s.logger.Infof("context compressed execution_id=%s entriesBefore=%d entriesAfter=%d", execution.ID, len(value.Entries), len(compressed.Entries))
	return nil
}
