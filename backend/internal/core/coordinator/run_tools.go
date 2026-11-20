package coordinator

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/entry"
	"praxis/internal/core/tool"
	"praxis/internal/core/tools"
	"praxis/internal/identity"
)

// bindRunTools 将协作工具绑定到本次 Run，再注册工具发现入口。
func (s *Coordinator) bindRunTools(run *agentrun.Run, active *activeRun) (*tool.ToolRegistry, error) {
	var collaboration []tool.Tool
	if run.Main() {
		collaboration = tools.SubagentMainTools(tools.SubagentParentOperations{
			Start: func(ctx context.Context, assignment tools.SubagentAssignment, callID string) (tools.SubagentIDs, error) {
				return s.startSubagent(ctx, run, active, assignment, callID)
			},
			Wait: func(ctx context.Context, ids []identity.AgentID, all bool) ([]entry.SubagentReceipt, error) {
				run.BeginWaiting()
				defer run.EndWaiting()
				return run.WaitSubagents(ctx, ids, all)
			},
			Send:   run.SendToSubagent,
			Cancel: run.CancelSubagent,
		})
	} else {
		collaboration = tools.SubagentDelegateTools(run.ReportToParent)
	}
	for _, candidate := range collaboration {
		if err := active.allowedTools.RegisterRunTool(candidate); err != nil {
			return nil, err
		}
	}
	return active.allowedTools.RegisterListTools()
}
