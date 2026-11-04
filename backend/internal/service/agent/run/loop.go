package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// Execute 准备初始历史和事实提交入口，再交给 Loop 完成多轮模型与工具迭代。
// 已有最终回答时只允许轮后摘要，不再请求模型执行用户任务。
func (s *Service) Execute(ctx context.Context, run *agentrun.Run, runner *loop.Runner, executor loop.ToolExecutor) (err error) {
	var turnID identity.TurnID
	defer func() {
		if err != nil {
			s.emitError(run, turnID, err)
		}
	}()
	record := run.Snapshot()
	if !record.Active() {
		return loop.NewFailure(loop.FailureContract, errorcode.ErrRequestConflict)
	}
	if record.Progress.FinalAssistantID == "" {
		input, inputErr := s.loopInput(ctx, run)
		if inputErr != nil {
			return inputErr
		}
		params, deps := s.modelRunParams(run)
		turnID, err = runner.Run(ctx, params, deps, input, executor)
		if err != nil {
			return err
		}
	}
	if compressionErr := s.Compact(ctx, run, runner); compressionErr != nil {
		if loop.FailureClassification(compressionErr) == loop.FailureStorage {
			return compressionErr
		}
		s.logger.Errorf("context compression failed run=%s; completed answer preserved: %v", record.ID, compressionErr)
	}
	return nil
}
