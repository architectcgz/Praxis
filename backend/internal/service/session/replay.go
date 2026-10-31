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
	state       *historyState
	calls       []replayedToolCall
	callIndexes map[sessionentry.Key]int
}

type replayedToolCall struct {
	call      sessionentry.ToolCall
	completed bool
}

// apply 是日志到业务状态的转换边界，发布状态前校验序号、身份与载荷。
func (r *logReplay) apply(line []byte) error {
	state := r.state
	sessionID := state.session.ID
	sessionKey := sessionID.String()
	var event storedEvent
	if err := decodeJSON(line, &event); err != nil {
		return fmt.Errorf("expected a single storage event: %w", err)
	}
	if state.sequence == ^uint64(0) || event.Sequence != state.sequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
		return errors.New("invalid event identity or sequence")
	}
	if event.Key == "" || strings.TrimSpace(event.Key) != event.Key {
		return errors.New("invalid object key")
	}
	switch event.Collection {
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
		state.setExecution(value)
	case "entry":
		var value entryRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "entry.appended", encodedEntryKey(value.AgentID, value.Data.ID), sessionKey, sessionKey); err != nil {
			return err
		}
		if value.Data.Sequence != event.Sequence {
			return errors.New("message sequence does not match event sequence")
		}
		if state.entryIndex(value.AgentID, value.Data.ID) >= 0 {
			return errors.New("duplicate entry append identity")
		}
		if err := r.matchToolEntry(value); err != nil {
			return err
		}
		state.entries = append(state.entries, value)
	case "context":
		var value contextRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "context.saved", sessionKey, sessionKey, sessionKey); err != nil {
			return err
		}
		if value.ThroughSequence != event.Sequence-1 {
			return errors.New("saved context boundary does not match event sequence")
		}
		for _, entry := range value.Entries {
			if entry.Sequence >= event.Sequence {
				return errors.New("saved context entry must precede its save event")
			}
		}
		state.context = &value
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
		state.setUsage(value)
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
		state.setTiming(value)
	default:
		return fmt.Errorf("unknown stored collection %q", event.Collection)
	}
	state.sequence = event.Sequence
	return nil
}

// matchToolEntry 只检查历史中的身份关系，不重新执行审批或准入。
func (r *logReplay) matchToolEntry(value entryRecord) error {
	session := r.state.session
	calls := value.Data.ToolCalls(session.ID, value.AgentID)
	if len(calls) == 0 && value.Data.Role != role.Tool {
		return nil
	}
	if session.MainAgentID() != value.AgentID {
		return errors.New("tool entry does not belong to the session main agent")
	}
	index := r.state.executionIndex(identity.AgentExecutionID(value.Data.ExecutionID))
	if index < 0 {
		return errors.New("tool entry execution is missing")
	}
	execution := r.state.executions[index]
	if execution.AgentID != value.AgentID || execution.RequestID.String() != value.Data.RequestID {
		return errors.New("tool entry execution ownership does not match its session, agent or request")
	}
	for _, call := range calls {
		if _, exists := r.callIndexes[call.Key]; exists {
			return fmt.Errorf("duplicate tool call %s in execution %s", call.CallID, call.ExecutionID)
		}
		r.callIndexes[call.Key] = len(r.calls)
		r.calls = append(r.calls, replayedToolCall{call: call})
	}
	for _, block := range value.Data.Content {
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
		if call.AgentID != value.AgentID || call.RequestID != value.Data.RequestID || call.TurnID.String() != value.Data.TurnID || string(call.Name) != block.Name {
			return fmt.Errorf("tool result %s ownership does not match its call in execution %s", key.CallID, key.ExecutionID)
		}
		prior.completed = true
	}
	return nil
}

// repairToolResults 由会话业务恢复流程决定补写中断回执，不恢复工具执行。
func (r *logReplay) repairToolResults(ctx context.Context, h *sessionHistory) error {
	for _, pending := range r.calls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if pending.completed {
			continue
		}
		call := pending.call
		if r.state.session.State != sessionmodel.SessionActive {
			return fmt.Errorf("cannot repair tool call %s in inactive session %s", call.CallID, call.SessionID)
		}
		_, _, err := h.appendEntryLocked(ctx, entryRecord{
			AgentID: call.AgentID,
			Data:    call.ResultEntry(tool.NewToolError("tool_interrupted", "The previous executor exited without a reliable tool result.")),
		})
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
