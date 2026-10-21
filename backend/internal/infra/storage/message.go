package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"strings"
	"time"
)

// messageRecord 保留日志既有格式，Session 归属来自文件 Scope。
type messageRecord struct {
	OwnerID string               `json:"owner_id,omitempty"`
	AgentID identity.AgentID     `json:"agent_id"`
	Data    messagemodel.Message `json:"message"`
}

type MessageRepository struct{ s *Store }

func messageKey(owner, id string) string { return fmt.Sprintf("%d:%s%s", len(owner), owner, id) }
func messageEntityKey(scope, owner, id string) entityKey {
	return entityKey{"message", scope + "\x00" + messageKey(owner, id)}
}

func (r MessageRepository) Append(ctx context.Context, value messagemodel.Record) (stored messagemodel.Message, inserted bool, err error) {
	scope, owner := value.Stream.SessionID.String(), value.Stream.OwnerAgentID.String()
	if scope == "" || value.AgentID == "" || owner != "" && owner != value.AgentID.String() {
		return stored, false, errors.New("message stream ownership is required")
	}
	err = r.s.view(ctx, func() error {
		key := messageEntityKey(scope, owner, value.Data.ID)
		prior, err := r.s.payload(key)
		if err == nil {
			var record messageRecord
			if err := decodeJSON(prior, &record); err != nil {
				return err
			}
			candidate := value.Data
			candidate.Sequence, candidate.CreatedAt = record.Data.Sequence, record.Data.CreatedAt
			if record.AgentID != value.AgentID || !same(record.Data, candidate) {
				return fault.ErrRequestConflict
			}
			stored = record.Data
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		// 幂等回读先于状态检查；新输出必须在当前执行仍允许写入时提交。
		if value.Data.Role == messagemodel.RoleAssistant {
			execution, err := getLocked[agentmodel.Execution](r.s, "agent_execution", value.Data.ExecutionID)
			if err != nil {
				return err
			}
			if !execution.Active() || execution.Progress.FinalAssistantID != "" {
				return fault.ErrRequestConflict
			}
		}
		data := value.Data
		data.Sequence = r.s.log.Sequence(scope) + 1
		if data.CreatedAt.IsZero() {
			data.CreatedAt = time.Now().UTC()
		}
		record := messageRecord{OwnerID: owner, AgentID: value.AgentID, Data: data}
		if err := record.Validate(); err != nil {
			return err
		}
		if err := r.s.appendLocked(ctx, scope, "message", messageKey(owner, data.ID), "message.appended", record); err != nil {
			return err
		}
		// 返回值与 State 的 RawMessage 及调用方传入的 slice 均不共享可变引用。
		payload, err := r.s.payload(key)
		if err != nil {
			return err
		}
		if err := decodeJSON(payload, &record); err != nil {
			return err
		}
		stored, inserted = record.Data, true
		return nil
	})
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}

func (r MessageRepository) Get(ctx context.Context, stream messagemodel.StreamKey, id string) (messagemodel.Message, error) {
	record, err := get[messageRecord](ctx, r.s, "message", stream.SessionID.String()+"\x00"+messageKey(stream.OwnerAgentID.String(), id))
	return record.Data, err
}

func (r MessageRepository) List(ctx context.Context, query repository.MessageQuery) (page repository.MessagePage, err error) {
	err = r.s.view(ctx, func() error {
		keys := r.s.keys(lookupKey{kind: "messages-stream", owner: query.Stream.SessionID.String(), value: query.Stream.OwnerAgentID.String()})
		selected := make([]entityKey, 0, len(keys))
		for _, key := range keys {
			entry, _ := r.s.entry(key)
			if entry.eventSequence > query.After {
				var record messageRecord
				if err := decodeJSON(entry.payload, &record); err != nil {
					return err
				}
				if record.Data.Role == messagemodel.RoleUser && !query.IncludeUnadmitted {
					if id := record.Data.ExecutionID; id != "" {
						if _, exists := r.s.entry(entityKey{"agent_execution", id}); !exists {
							continue
						}
					}
				}
				selected = append(selected, key)
			}
		}
		sorted(selected, func(a, b entityKey) int {
			left, _ := r.s.entry(a)
			right, _ := r.s.entry(b)
			return cmp.Compare(left.eventSequence, right.eventSequence)
		})
		if len(selected) > 0 {
			entry, _ := r.s.entry(selected[len(selected)-1])
			page.SequenceBoundary = entry.eventSequence
		}
		if query.Limit > 0 {
			if query.Tail {
				selected = selected[max(0, len(selected)-query.Limit):]
			} else {
				selected = selected[:min(len(selected), query.Limit)]
			}
		}
		page.Messages = make([]messagemodel.Message, 0, len(selected))
		for _, key := range selected {
			data, err := r.s.payload(key)
			if err != nil {
				return err
			}
			var record messageRecord
			if err := decodeJSON(data, &record); err != nil {
				return err
			}
			page.Messages = append(page.Messages, record.Data)
		}
		return nil
	})
	return page, err
}

// Validate 只在磁盘读取入口检查消息结构和流身份。
func (r messageRecord) Validate() error {
	if r.AgentID == "" || r.AgentID.String() != strings.TrimSpace(r.AgentID.String()) || strings.ContainsAny(r.AgentID.String(), "\x00\r\n") {
		return errors.New("message agent identity must be canonical and nonempty")
	}
	if r.Data.Role == messagemodel.RoleAssistant && (r.Data.AuthorKind != messagemodel.AuthorAgent || r.Data.AuthorID != r.AgentID.String()) {
		return errors.New("assistant author does not match the message agent")
	}
	if err := validateMessageData(r.Data); err != nil {
		return err
	}
	for _, block := range r.Data.Blocks {
		if block.Kind == messagemodel.BlockReasoningState {
			if err := validateReasoningState(block.ReasoningState); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateMessageData 在消息写入与磁盘恢复边界检查身份、角色及内容结构。
func validateMessageData(m messagemodel.Message) error {
	if m.ID == "" || m.Sequence == 0 || m.CreatedAt.IsZero() {
		return errors.New("message identity, sequence and creation time are required")
	}
	for _, value := range []string{
		m.ID,
		m.RequestID,
		m.SourceTaskID,
		m.HandoffID,
		m.ExecutionID,
		m.TurnID,
		m.AuthorID,
	} {
		if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("message identity fields must be canonical")
		}
	}
	switch m.Role {
	case messagemodel.RoleUser, messagemodel.RoleAssistant, messagemodel.RoleTool:
	default:
		return errors.New("message role is invalid")
	}
	switch m.AuthorKind {
	case messagemodel.AuthorUser, messagemodel.AuthorAgent, messagemodel.AuthorTool, messagemodel.AuthorSystem:
	default:
		return errors.New("message author is invalid")
	}
	if len(m.Blocks) == 0 && !(m.Role == messagemodel.RoleAssistant && (m.StopReason == "completed" || m.StopReason == "stop_sequence")) {
		return errors.New("message blocks are required")
	}
	for _, block := range m.Blocks {
		if err := validateMessageBlock(m.Role, block); err != nil {
			return err
		}
	}
	return nil
}

func validateMessageBlock(role messagemodel.Role, block messagemodel.Block) error {
	if block.Kind != messagemodel.BlockReasoningState && block.ReasoningState != nil {
		return errors.New("reasoning state requires a reasoning state message block")
	}
	switch block.Kind {
	case messagemodel.BlockReasoningState:
		if role != messagemodel.RoleAssistant || block.ReasoningState == nil || block.Text != "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid reasoning state block")
		}
	case messagemodel.BlockText:
		if strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid text block")
		}
	case messagemodel.BlockThinking:
		if role != messagemodel.RoleAssistant || strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid thinking block")
		}
	case messagemodel.BlockToolCall:
		if role != messagemodel.RoleAssistant || block.CallID == "" || block.Name == "" || block.CallID != strings.TrimSpace(block.CallID) || block.Name != strings.TrimSpace(block.Name) || block.IsError || len(block.Input) > 0 && !json.Valid(block.Input) {
			return errors.New("message contains an invalid tool call block")
		}
	case messagemodel.BlockToolResult:
		if role != messagemodel.RoleTool || block.CallID == "" || block.Name == "" || block.CallID != strings.TrimSpace(block.CallID) || block.Name != strings.TrimSpace(block.Name) || len(block.Input) > 0 {
			return errors.New("message contains an invalid tool result block")
		}
	default:
		return errors.New("message contains an unknown block kind")
	}
	return nil
}
