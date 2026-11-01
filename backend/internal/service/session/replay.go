package session

import (
	"context"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// logReplay 只在单个日志重放期间持有调用匹配索引，完成后即释放。
type logReplay struct {
	session     *sessionmodel.Data
	calls       []replayedToolCall
	callIndexes map[sessionentry.Key]int
}

type replayedToolCall struct {
	call      sessionentry.ToolCall
	completed bool
}

// apply 按日志顺序还原已提交的会话事实。
func (r *logReplay) apply(event sessionmodel.Event) error {
	owner := r.session
	switch event.Type {
	case sessionmodel.EventBranchCreated:
		owner.AcceptBranch(event.Payload.(sessionmodel.Branch))
	case sessionmodel.EventExecutionSaved:
		owner.SetExecution(event.Payload.(agentexecution.ExecutionRecord))
	case sessionmodel.EventEntryAppended:
		value := event.Payload.(sessionentry.ContextEntry)
		r.trackToolEntry(value)
		owner.AcceptEntry(value)
	case sessionmodel.EventUsageSaved:
		owner.SetUsage(event.Payload.(providerapi.UsageRecord))
	case sessionmodel.EventTimingStarted, sessionmodel.EventTimingFinished:
		owner.SetTiming(event.Payload.(timing.Record))
	}
	return nil
}

// trackToolEntry 记录调用完成情况，供日志加载后补写中断回执。
func (r *logReplay) trackToolEntry(value sessionentry.ContextEntry) {
	for _, call := range value.ToolCalls(r.session.ID, r.session.MainAgentID()) {
		r.callIndexes[call.Key] = len(r.calls)
		r.calls = append(r.calls, replayedToolCall{call: call})
	}
	for _, block := range value.Content {
		if block.Kind != sessionentry.BlockToolResult {
			continue
		}
		key := sessionentry.Key{
			ExecutionID: identity.AgentExecutionID(value.ExecutionID),
			CallID:      block.CallID,
		}
		r.calls[r.callIndexes[key]].completed = true
	}
}

// repairToolResults 由会话业务恢复流程决定补写中断回执，不恢复工具执行。
func (r *logReplay) repairToolResults(ctx context.Context, owner *sessionmodel.Session, sessionState sessionmodel.SessionState) error {
	for _, pending := range r.calls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if pending.completed {
			continue
		}
		call := pending.call
		if sessionState != sessionmodel.SessionActive {
			return fmt.Errorf("cannot repair tool call %s in inactive session %s", call.CallID, call.SessionID)
		}
		execution := r.session.Executions[r.session.ExecutionIndex(call.ExecutionID)]
		branch := r.session.Branch(execution.BranchID)
		entry := call.ResultEntry(tool.NewToolError("tool_interrupted", "The previous executor exited without a reliable tool result."))
		_, _, err := owner.AppendEntry(ctx, branch.ID, entry)
		if err != nil {
			return fmt.Errorf("save interrupted result for execution %s call %s: %w", call.ExecutionID, call.CallID, err)
		}
	}
	return nil
}
