package run

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
)

// Compact 只在完整用户轮次结束后生成并记录摘要，使用与其他 Entry 相同的接纳顺序。
func (s *Service) Compact(ctx context.Context, run *agentrun.Run, runner *loop.Runner) (err error) {
	input, err := s.loopInput(ctx, run)
	if err != nil {
		return err
	}
	current := appcontext.BuildContext(input.Entries)
	content := appcontext.ContentFromEntries(input.SystemPrompt, input.InputEntryID, current.Entries())
	params, deps := s.modelRunParams(run)
	value := appcontext.ModelContext{
		SystemPrompt: content.SystemPrompt,
		Entries:      content.Entries,
	}
	event := hook.ContextCompression{
		Identity: runIdentity(run, ""),
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
	span := timing.NewSpan()
	span.Start()
	generated, needed, err := runner.Compress(ctx, params, deps, value)
	measured := span.End()
	if err != nil {
		return loop.NewModelFailure(err)
	}
	if !needed {
		return nil
	}
	entry := generated.Entries[0]
	entry.Timing = measured
	entry.AuthorID, entry.RunID, entry.RequestID = params.AgentID.String(), params.ID.String(), params.RequestID.String()
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return loop.NewFailure(loop.FailureContract, err)
	}
	digest := sha256.Sum256(encoded)
	entry.ID = fmt.Sprintf("summary:%s:%x", params.ID, digest[:12])
	accepted, _, err := s.appendEntry(ctx, run, entry)
	if accepted != nil {
		compressed, changed = generated, true
		compressed.Entries[0] = *accepted
	}
	if err != nil {
		return err
	}
	s.logger.Infof("context compressed run_id=%s entriesBefore=%d entriesAfter=%d", params.ID, len(value.Entries), len(compressed.Entries))
	return nil
}
