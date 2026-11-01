package session

import (
	"context"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/core/workflow"
	"praxis/internal/identity"
	"strings"
)

// logReplay 只在单个日志重放期间持有调用匹配索引，完成后即释放。
type logReplay struct {
	session     *sessionmodel.Session
	calls       []replayedToolCall
	callIndexes map[sessionentry.Key]int
}

type replayedToolCall struct {
	call      sessionentry.ToolCall
	completed bool
}

// apply 是日志到业务状态的转换边界，发布状态前校验序号、身份与载荷。
func (r *logReplay) apply(line []byte) error {
	owner := r.session
	sessionID := owner.ID
	sessionKey := sessionID.String()
	var event storedEvent
	if err := decodeJSON(line, &event); err != nil {
		return fmt.Errorf("expected a single storage event: %w", err)
	}
	if event.CreatedAt.IsZero() {
		return errors.New("event creation time is required")
	}
	if event.Key == "" || strings.TrimSpace(event.Key) != event.Key {
		return errors.New("invalid object key")
	}
	switch event.Collection {
	case "branch":
		var value sessionmodel.Branch
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := eventIdentity(event, "branch.created", value.ID.String(), sessionKey, sessionKey); err != nil {
			return err
		}
		if err := validateBranch(owner, value); err != nil {
			return err
		}
		owner.AcceptBranch(value)
	case "agent_execution":
		var value agentexecution.ExecutionRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "agent_execution.saved", value.ID.String(), value.SessionID.String(), sessionKey); err != nil {
			return err
		}
		if err := validateExecutionOwnership(owner, value); err != nil {
			return err
		}
		owner.SetExecution(value)
	case "entry":
		var value sessionentry.ContextEntry
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := validateContextEntry(value); err != nil {
			return err
		}
		if err := eventIdentity(event, "entry.appended", value.ID, sessionKey, sessionKey); err != nil {
			return err
		}
		if _, found := owner.Entry(value.ID); found {
			return errors.New("duplicate entry append identity")
		}
		branch := owner.Branch(value.BranchID)
		if branch == nil || branch.HeadEntryID != value.ParentID {
			return errors.New("entry does not advance its branch head")
		}
		if err := validateEntry(owner, value); err != nil {
			return err
		}
		if err := r.matchToolEntry(value); err != nil {
			return err
		}
		owner.AcceptEntry(value)
	case "usage":
		var value providerapi.UsageRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "usage.saved", value.Key(), value.SessionID, sessionKey); err != nil {
			return err
		}
		if err := validateUsage(owner, value); err != nil {
			return err
		}
		owner.SetUsage(value)
	case "wait":
		var value workflow.WaitCondition
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "wait.saved", value.ID.String(), sessionKey, sessionKey); err != nil {
			return err
		}
		// 重启不恢复审批等待，只检查该记录没有破坏历史完整性。
	case "agent_execution_timing":
		var value timing.Record
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		typ := "agent_execution.timing_finished"
		if value.Status == timing.Running {
			typ = "agent_execution.timing_started"
		}
		if value.Status == timing.Interrupted {
			return errors.New("interrupted timing is a read projection")
		}
		if err := eventIdentity(event, typ, timingKey(value.ExecutionID, value.ID), sessionKey, sessionKey); err != nil {
			return err
		}
		if owner.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID)) < 0 {
			return errors.New("timing execution is missing")
		}
		owner.SetTiming(value)
	default:
		return fmt.Errorf("unknown stored collection %q", event.Collection)
	}
	return nil
}

// matchToolEntry 只检查历史中的身份关系，不重新执行审批或准入。
func (r *logReplay) matchToolEntry(value sessionentry.ContextEntry) error {
	session := r.session
	agentID := session.MainAgentID()
	calls := value.ToolCalls(session.ID, agentID)
	if len(calls) == 0 && value.Role != role.Tool {
		return nil
	}
	index := r.session.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID))
	if index < 0 {
		return errors.New("tool entry execution is missing")
	}
	execution := r.session.Executions[index]
	if execution.AgentID != agentID || execution.RequestID.String() != value.RequestID {
		return errors.New("tool entry execution ownership does not match its session, agent or request")
	}
	for _, call := range calls {
		if _, exists := r.callIndexes[call.Key]; exists {
			return fmt.Errorf("duplicate tool call %s in execution %s", call.CallID, call.ExecutionID)
		}
		r.callIndexes[call.Key] = len(r.calls)
		r.calls = append(r.calls, replayedToolCall{call: call})
	}
	for _, block := range value.Content {
		if block.Kind != sessionentry.BlockToolResult {
			continue
		}
		key := sessionentry.Key{ExecutionID: execution.ID, CallID: block.CallID}
		index, exists := r.callIndexes[key]
		if !exists {
			return fmt.Errorf("tool result %s has no preceding call in execution %s", key.CallID, key.ExecutionID)
		}
		prior := &r.calls[index]
		if prior.completed {
			return fmt.Errorf("duplicate tool result %s in execution %s", key.CallID, key.ExecutionID)
		}
		call := prior.call
		if call.AgentID != agentID || call.RequestID != value.RequestID || call.TurnID.String() != value.TurnID || string(call.Name) != block.Name {
			return fmt.Errorf("tool result %s ownership does not match its call in execution %s", key.CallID, key.ExecutionID)
		}
		prior.completed = true
	}
	return nil
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

func eventIdentity(event storedEvent, typ, key, owner, sessionID string) error {
	if event.Type != typ {
		return errors.New("unknown event type")
	}
	if event.Key != key || owner != sessionID {
		return errors.New("object identity or session ownership does not match event")
	}
	return nil
}
