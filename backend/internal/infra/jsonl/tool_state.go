package jsonl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolexecution "praxis/internal/core/tool_execution"
)

// 复用 Store 的统一事件信封，正文不重复存储 Sequence、事件 ID 或时间。
type toolExecutionRecord struct {
	Call     toolexecution.CallKey      `json:"call"`
	Prepared *toolexecution.Preparation `json:"prepared,omitempty"`
	Approval *contracts.ApprovalRecord  `json:"approval,omitempty"`
}

func decodeToolRecord(scope string, event Event) (toolexecution.Record, error) {
	value := toolexecution.Record{
		Sequence:  event.Sequence,
		ID:        event.ID,
		Type:      toolexecution.RecordType(event.Type),
		SessionID: contracts.SessionID(scope),
		CreatedAt: event.CreatedAt,
	}
	switch event.Collection {
	case "tool_execution":
		var record toolExecutionRecord
		if err := decodeJSON(event.Payload, &record); err != nil {
			return value, err
		}
		if record.Call.SessionID.String() != scope || event.Key != event.ID {
			return value, errors.New("tool record ownership or identity mismatch")
		}
		value.AgentID, value.TaskID, value.CallID = record.Call.AgentID, record.Call.TaskID, record.Call.CallID
		value.Prepared, value.Approval = record.Prepared, record.Approval
	case "message":
		var record messageRecord
		if err := decodeJSON(event.Payload, &record); err != nil {
			return value, err
		}
		if event.Type != "message.appended" || event.Key != messageKey(record.OwnerID, record.Data.ID) {
			return value, errors.New("tool declaration or result message identity mismatch")
		}
		value.Type = toolexecution.RecordMessage
		value.AgentID = record.AgentID
		value.TaskID = contracts.TaskID(record.Data.TaskID)
		value.Message, value.Outcome = &record.Data, record.Outcome
		if record.Data.Role == sessionmodel.RoleTool && len(record.Data.Blocks) == 1 {
			value.CallID = record.Data.Blocks[0].CallID
		}
	default:
		return value, errors.New("event does not contain a tool execution record")
	}
	return value, nil
}

func toolRecordCallKeys(scope string, event Event) ([]toolexecution.CallKey, error) {
	if event.Collection != "message" && event.Collection != "tool_execution" {
		return nil, nil
	}
	value, err := decodeToolRecord(scope, event)
	if err != nil {
		return nil, err
	}
	if value.Message == nil || value.Message.Role == sessionmodel.RoleTool {
		return []toolexecution.CallKey{value.Key()}, nil
	}
	var keys []toolexecution.CallKey
	for _, block := range value.Message.Blocks {
		if block.Kind == sessionmodel.BlockToolCall {
			key := value.Key()
			key.CallID = block.CallID
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Store) requireRecordBackedTask(ctx context.Context, taskID contracts.TaskID) error {
	// 切换 Executor 前，禁止同一 Task 同时采用实体状态与事件状态，避免两条路径各自授予启动资格。
	if len(s.keys(ctx, lookupKey{kind: "tools-task", owner: taskID.String()})) != 0 {
		return errors.New("task tool calls are managed by the invocation repository")
	}
	return nil
}

func (s *Store) rebuildToolState(ctx context.Context, key toolexecution.CallKey, reader recordReader) (toolexecution.State, error) {
	if err := key.Validate(); err != nil {
		return toolexecution.State{}, err
	}
	if err := s.requireRecordBackedTask(ctx, key.TaskID); err != nil {
		return toolexecution.State{}, err
	}
	tx := s.tx(ctx)
	if s.index.deleted[key.SessionID.String()] || tx.deletedScope && tx.scope == key.SessionID.String() {
		return toolexecution.State{}, contracts.ErrNotFound
	}
	states := make(map[toolexecution.CallKey]toolexecution.State)
	for _, ref := range s.index.toolRecords[key.SessionID.String()][key] {
		record, err := s.readRecord(ref, reader)
		if err != nil {
			return toolexecution.State{}, err
		}
		if ref.EventIndex < 0 || ref.EventIndex >= len(record.Events) {
			return toolexecution.State{}, errors.New("invalid tool record position")
		}
		event := record.Events[ref.EventIndex]
		if event.Sequence != ref.Sequence || event.ID != fmt.Sprintf("event:%d", ref.Sequence) {
			return toolexecution.State{}, errors.New("tool record position identity mismatch")
		}
		keys, err := toolRecordCallKeys(ref.Scope, event)
		if err != nil {
			return toolexecution.State{}, err
		}
		if !slices.Contains(keys, key) {
			return toolexecution.State{}, errors.New("tool record position ownership mismatch")
		}
		value, err := decodeToolRecord(ref.Scope, event)
		if err != nil {
			return toolexecution.State{}, err
		}
		if err := toolexecution.ApplyRecord(states, value); err != nil {
			return toolexecution.State{}, err
		}
	}
	for _, event := range tx.events {
		if tx.scope != key.SessionID.String() {
			continue
		}
		keys, err := toolRecordCallKeys(tx.scope, event)
		if err != nil {
			return toolexecution.State{}, err
		}
		if !slices.Contains(keys, key) {
			continue
		}
		value, err := decodeToolRecord(tx.scope, event)
		if err != nil {
			return toolexecution.State{}, err
		}
		if err := toolexecution.ApplyRecord(states, value); err != nil {
			return toolexecution.State{}, err
		}
	}
	state, ok := states[key]
	if !ok {
		return state, contracts.ErrNotFound
	}
	return state, nil
}

func (s *Store) rebuildToolStates(ctx context.Context, sessionID contracts.SessionID, taskID contracts.TaskID) ([]toolexecution.State, error) {
	if !validScope(sessionID.String()) {
		return nil, errors.New("invalid tool execution session identity")
	}
	tx := s.tx(ctx)
	if s.index.deleted[sessionID.String()] || tx.deletedScope && tx.scope == sessionID.String() {
		return nil, contracts.ErrNotFound
	}
	positions := make(map[recordRef]struct{})
	keys := make(map[toolexecution.CallKey]struct{})
	for key, history := range s.index.toolRecords[sessionID.String()] {
		if taskID != "" && key.TaskID != taskID {
			continue
		}
		keys[key] = struct{}{}
		for _, ref := range history {
			positions[ref] = struct{}{}
		}
	}
	// 多个调用可以来自同一条 Assistant 消息，批量恢复时每条记录只处理一次。
	refs := slices.SortedFunc(maps.Keys(positions), func(a, b recordRef) int { return cmp.Compare(a.Sequence, b.Sequence) })
	states := make(map[toolexecution.CallKey]toolexecution.State)
	reader := make(recordReader)
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := s.readRecord(ref, reader)
		if err != nil {
			return nil, err
		}
		if ref.EventIndex < 0 || ref.EventIndex >= len(record.Events) {
			return nil, errors.New("invalid tool record position")
		}
		event := record.Events[ref.EventIndex]
		if event.Sequence != ref.Sequence || event.ID != fmt.Sprintf("event:%d", ref.Sequence) {
			return nil, errors.New("tool record position identity mismatch")
		}
		value, err := decodeToolRecord(ref.Scope, event)
		if err != nil {
			return nil, err
		}
		if taskID != "" && value.TaskID != taskID {
			return nil, errors.New("tool record position task ownership mismatch")
		}
		if err := toolexecution.ApplyRecord(states, value); err != nil {
			return nil, err
		}
	}
	if tx.scope == sessionID.String() {
		for _, event := range tx.events {
			calls, err := toolRecordCallKeys(tx.scope, event)
			if err != nil {
				return nil, err
			}
			selected := false
			for _, key := range calls {
				if (taskID == "" || key.TaskID == taskID) && len(s.keys(ctx, lookupKey{kind: "tools-task", owner: key.TaskID.String()})) == 0 {
					keys[key] = struct{}{}
					selected = true
				}
			}
			if !selected {
				continue
			}
			value, err := decodeToolRecord(tx.scope, event)
			if err != nil {
				return nil, err
			}
			if err := toolexecution.ApplyRecord(states, value); err != nil {
				return nil, err
			}
		}
	}
	result := make([]toolexecution.State, 0, len(keys))
	for key := range keys {
		state, ok := states[key]
		if !ok {
			return nil, errors.New("tool records do not match the indexed call identity")
		}
		result = append(result, state)
	}
	return sorted(result, func(a, b toolexecution.State) int {
		return cmp.Or(cmp.Compare(a.DeclaredSequence, b.DeclaredSequence), cmp.Compare(a.CallIndex, b.CallIndex))
	}), nil
}

func (s *Store) validateToolRecords(ctx context.Context) error {
	tx := s.tx(ctx)
	keys := make(map[toolexecution.CallKey]struct{})
	for _, event := range tx.events {
		calls, err := toolRecordCallKeys(tx.scope, event)
		if err != nil {
			return err
		}
		for _, key := range calls {
			if len(s.keys(ctx, lookupKey{kind: "tools-task", owner: key.TaskID.String()})) != 0 {
				if event.Collection == "tool_execution" {
					return errors.New("task cannot mix invocation persistence and tool execution records")
				}
				continue
			}
			keys[key] = struct{}{}
		}
	}
	tasks := make(map[contracts.TaskID]struct{})
	for key := range keys {
		tasks[key.TaskID] = struct{}{}
	}
	for taskID := range tasks {
		usage, err := (ToolExecutionRepository{s}).ToolUsage(ctx, contracts.SessionID(tx.scope), taskID)
		if err != nil {
			return err
		}
		if usage.Calls > taskmodel.MaxToolCalls {
			return contracts.New(contracts.TaskResourceLimit, "tool call limit exceeded")
		}
	}
	return nil
}

func (s *Store) indexToolRecords(tx *transaction, offset, length int64) {
	for n, event := range tx.events {
		keys, _ := toolRecordCallKeys(tx.scope, event) // 事务提交前已完整校验。
		for _, key := range keys {
			if len(s.index.groups[lookupKey{kind: "tools-task", owner: key.TaskID.String()}]) != 0 {
				continue
			}
			if s.index.toolRecords[tx.scope] == nil {
				s.index.toolRecords[tx.scope] = make(map[toolexecution.CallKey][]recordRef)
			}
			s.index.toolRecords[tx.scope][key] = append(s.index.toolRecords[tx.scope][key], recordRef{
				Scope:      tx.scope,
				Offset:     offset,
				Length:     length,
				EventIndex: n,
				Sequence:   event.Sequence,
			})
		}
	}
}
