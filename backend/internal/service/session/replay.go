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
func (r *logReplay) apply(line []byte) error {
	owner := r.session
	var event storedEvent
	if err := decodeJSON(line, &event); err != nil {
		return fmt.Errorf("expected a single storage event: %w", err)
	}
	switch event.Type {
	case "branch.created":
		var value sessionmodel.Branch
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		owner.AcceptBranch(value)
	case "agent_execution.saved":
		var value agentexecution.ExecutionRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		owner.SetExecution(value)
	case "entry.appended":
		var value sessionentry.ContextEntry
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		r.trackToolEntry(value)
		owner.AcceptEntry(value)
	case "usage.saved":
		var value providerapi.UsageRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		owner.SetUsage(value)
	case "wait.saved":
		// 重启不恢复审批等待。
	case "agent_execution.timing_started", "agent_execution.timing_finished":
		var value timing.Record
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		owner.SetTiming(value)
	default:
		return fmt.Errorf("unknown stored event type %q", event.Type)
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
func (r *logReplay) repairToolResults(ctx context.Context, owner *Session, sessionState sessionmodel.SessionState) error {
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
		entry.BranchID, entry.ParentID = branch.ID, branch.HeadEntryID
		_, _, err := owner.appendEntry(ctx, entry)
		if err != nil {
			return fmt.Errorf("save interrupted result for execution %s call %s: %w", call.ExecutionID, call.CallID, err)
		}
	}
	return nil
}
