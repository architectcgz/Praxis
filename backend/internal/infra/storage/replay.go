package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/identity"
	"strings"
)

// logReplay 只在单个日志重放期间持有调用匹配状态，完成后即释放。
type logReplay struct {
	state       *state
	scope       logScope
	calls       []replayedToolCall
	callIndexes map[sessionentry.Key]int
}

type replayedToolCall struct {
	call      sessionentry.ToolCall
	completed bool
}

// loadSessionLocked 调用方持有 Store 写锁；失败时清除本次重放状态，不发布部分历史。
func (s *Store) loadSessionLocked(ctx context.Context, id identity.SessionID) error {
	if _, loaded := s.state.sessions[id]; loaded {
		return nil
	}
	session, err := s.sessionRecord(ctx, id)
	if err != nil {
		return err
	}
	path := s.sessionLogPath(id.String(), session.ProjectID.String())
	if err := checkRegularFile(path); err != nil {
		return err
	}
	s.state.sessions[id] = session
	if err := s.replayLog(ctx, path, logScope{ID: id.String()}); err != nil {
		s.state.removeSession(id)
		return err
	}
	return nil
}

// replayLog 逐行校验并恢复状态，读到末尾后修复缺失回执；会话失败只影响本次访问。
func (s *Store) replayLog(ctx context.Context, path string, scope logScope) (err error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	replay := logReplay{
		state:       &s.state,
		scope:       scope,
		callIndexes: make(map[sessionentry.Key]int),
	}
	reader := bufio.NewReader(file)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 {
				if err := s.truncateTail(file, offset, line); err != nil {
					return fmt.Errorf("%s offset %d: %w", path, offset, err)
				}
			}
			break
		}
		if readErr != nil {
			return readErr
		}
		if err := replay.apply(line); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		offset += int64(len(line))
	}
	if err := replay.repairToolResults(ctx, s); err != nil {
		return fmt.Errorf("repair log %s: %w", path, err)
	}
	return nil
}

// apply 是日志格式到类型化状态的唯一转换边界，同时匹配当前日志中的工具调用与回执。
func (r *logReplay) apply(line []byte) error {
	state, scope := r.state, r.scope
	var event storedEvent
	if err := decodeJSON(line, &event); err != nil {
		return fmt.Errorf("expected a single storage event: %w", err)
	}
	// 磁盘记录不能视为可信输入，必须在发布状态前确认序号与事件身份。
	sequence := state.sequences[scope.ID]
	if sequence == ^uint64(0) || event.Sequence != sequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
		return errors.New("invalid event identity or sequence")
	}
	if event.Key == "" || strings.TrimSpace(event.Key) != event.Key {
		return errors.New("invalid object key")
	}
	if (scope.ID == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return errors.New("invalid event file ownership")
	}
	sessionID := identity.SessionID(scope.ID)
	switch event.Collection {
	case "project":
		// 项目载荷中的审批字段仅接受读取，不参与配置恢复或授权决策。
		var stored struct {
			projectmodel.Project
			ApprovalMode json.RawMessage
		}
		if err := decodeJSON(event.Payload, &stored); err != nil {
			return err
		}
		value := stored.Project
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "project.saved", value.ID.String(), "", scope.ID); err != nil {
			return err
		}
		state.projects[value.ID] = value
	case "workspace":
		var value workspacemodel.Workspace
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "workspace.saved", value.ID.String(), "", scope.ID); err != nil {
			return err
		}
		state.workspaces[value.ID] = value
	case "agent_execution":
		var value agentexecution.ExecutionRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "agent_execution.saved", value.ID.String(), value.SessionID.String(), scope.ID); err != nil {
			return err
		}
		state.executions[value.ID] = value
	case "entry":
		if event.Type == "entry.updated" {
			var update toolResultUpdate
			if err := decodeJSON(event.Payload, &update); err != nil {
				return err
			}
			for _, field := range []string{update.AgentID.String(), update.EntryID} {
				if field == "" || field != strings.TrimSpace(field) || strings.ContainsAny(field, "\x00\r\n") {
					return errors.New("tool result update identity must be canonical and nonempty")
				}
			}
			if len(update.Text) > tool.MaxInlineResultBytes {
				return errors.New("tool result update exceeds the inline size limit")
			}
			key := entryKey{AgentID: update.AgentID, ID: update.EntryID}
			if err := eventIdentity(event, "entry.updated", encodedEntryKey(key), scope.ID, scope.ID); err != nil {
				return err
			}
			prior, ok := state.entries[key]
			if !ok {
				return errors.New("updated entry is missing its original append event")
			}
			data := &prior.Record.Data
			if prior.SessionID != sessionID || data.Role != role.Tool || len(data.Content) != 1 || data.Content[0].Kind != sessionentry.BlockToolResult {
				return errors.New("tool result update requires an existing tool receipt in the same session")
			}
			data.Content[0].Text = update.Text
			prior.UpdatedSequence = event.Sequence
			state.entries[key] = prior
		} else {
			var value entryRecord
			if err := decodeJSON(event.Payload, &value); err != nil {
				return err
			}
			if err := value.Validate(); err != nil {
				return err
			}
			key := entryKey{AgentID: value.AgentID, ID: value.Data.ID}
			if err := eventIdentity(event, "entry.appended", encodedEntryKey(key), scope.ID, scope.ID); err != nil {
				return err
			}
			if value.Data.Sequence != event.Sequence {
				return errors.New("message sequence does not match event sequence")
			}
			if _, exists := state.entries[key]; exists {
				return errors.New("duplicate entry append identity")
			}
			if err := r.matchToolEntry(value); err != nil {
				return err
			}
			state.entries[key] = storedEntry{
				SessionID:       sessionID,
				Record:          value,
				UpdatedSequence: event.Sequence,
			}
			state.entryOrder[sessionID] = append(state.entryOrder[sessionID], key)
		}
	case "context":
		var value contextRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "context.saved", scope.ID, scope.ID, scope.ID); err != nil {
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
		state.contexts[sessionID] = value
	case "usage":
		var value providerapi.UsageRecord
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "usage.saved", value.Key(), value.SessionID, scope.ID); err != nil {
			return err
		}
		state.usage[value.Key()] = value
	case "wait":
		var value workflow.WaitCondition
		if err := decodeJSON(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "wait.saved", value.ID.String(), scope.ID, scope.ID); err != nil {
			return err
		}
		state.waits[value.ID] = storedWait{SessionID: sessionID, Value: value}
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
		if err := eventIdentity(event, typ, timingKey(value.ExecutionID, value.ID), scope.ID, scope.ID); err != nil {
			return err
		}
		state.timings[timingKey(value.ExecutionID, value.ID)] = value
	default:
		return fmt.Errorf("unknown stored collection %q", event.Collection)
	}
	state.sequences[scope.ID] = event.Sequence
	return nil
}

// matchToolEntry 只检查工具记录的身份关系，不重跑历史审批或执行状态准入。
func (r *logReplay) matchToolEntry(value entryRecord) error {
	sessionID := identity.SessionID(r.scope.ID)
	calls := value.Data.ToolCalls(sessionID, value.AgentID)
	if len(calls) == 0 && value.Data.Role != role.Tool {
		return nil
	}
	session, ok := r.state.sessions[sessionID]
	if !ok || session.MainAgentID() != value.AgentID {
		return errors.New("tool entry does not belong to the session main agent")
	}
	execution, ok := r.state.executions[identity.AgentExecutionID(value.Data.ExecutionID)]
	if !ok || execution.SessionID != sessionID || execution.AgentID != value.AgentID || execution.RequestID.String() != value.Data.RequestID {
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

// repairToolResults 读完日志才确认缺失；按调用顺序追加确定身份的中断回执，成功落盘后更新内存。
func (r *logReplay) repairToolResults(ctx context.Context, storage *Store) error {
	for _, pending := range r.calls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if pending.completed {
			continue
		}
		call := pending.call
		if r.state.sessions[call.SessionID].State != sessionmodel.SessionActive {
			return fmt.Errorf("cannot repair tool call %s in inactive session %s", call.CallID, call.SessionID)
		}
		_, _, err := storage.appendEntryLocked(ctx, sessionentry.Record{
			SessionID: call.SessionID,
			AgentID:   call.AgentID,
			Data:      call.ResultEntry(tool.NewToolError("tool_interrupted", "The previous executor exited without a reliable tool result.")),
		})
		if err != nil {
			return fmt.Errorf("save interrupted result for execution %s call %s: %w", call.ExecutionID, call.CallID, err)
		}
	}
	return nil
}

// truncateTail 先同步尾行备份及其目录，再截断原日志，避免修复时丢失待检查的原始内容。
func (s *Store) truncateTail(file *os.File, offset int64, tail []byte) error {
	backup, err := os.CreateTemp(s.dataRoot, "incomplete-tail-*")
	if err != nil {
		return err
	}
	_, writeErr := backup.Write(tail)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		return err
	}
	if err := syncDirectory(s.dataRoot); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}

func eventIdentity(event storedEvent, typ, key, owner, scope string) error {
	if event.Type != typ {
		return errors.New("unknown event type")
	}
	if event.Key != key || owner != scope {
		return errors.New("object identity or session ownership does not match event")
	}
	return nil
}
