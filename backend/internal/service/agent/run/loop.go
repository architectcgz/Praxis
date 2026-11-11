package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// Execute 绑定历史读取和事实提交入口，再交给 Loop 完成多轮模型与工具迭代。
// 已有最终回答时直接返回，不再请求模型执行用户任务。
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
	if record.Progress.FinalAssistantID != "" {
		return nil
	}
	input, _ := run.Entry(record.InputEntryID)
	params, deps := s.modelRunParams(run)
	turnID, err = runner.Run(ctx, params, deps, input, executor)
	return err
}
