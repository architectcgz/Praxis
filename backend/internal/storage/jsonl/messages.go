package jsonl

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
	messagemodel "praxis/internal/message"
	"praxis/internal/toolcall"
	"strings"
	"time"
)

type messageRecord struct {
	OwnerID string               `json:"owner_id,omitempty"`
	AgentID identity.AgentID     `json:"agent_id"`
	Data    messagemodel.Message `json:"message"`
}

type MessageStreams struct{ s *Store }

func appendOwner(owner agentmodel.Agent) string {
	if owner.CanReadSessionContext() {
		return ""
	}
	return owner.ID.String()
}
func messageKey(owner, id string) string { return fmt.Sprintf("%d:%s%s", len(owner), owner, id) }
func messageEntityKey(scope, owner, id string) entityKey {
	return entityKey{"message", scope + "\x00" + messageKey(owner, id)}
}

func (r MessageStreams) Append(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, data messagemodel.Message) (stored messagemodel.Message, err error) {
	err = r.s.InTx(ctx, func(txCtx context.Context) error {
		owner, err := r.owner(txCtx, sessionID, agentID)
		if err != nil {
			return err
		}
		stored, _, err = r.s.appendMessage(txCtx, owner, data)
		return err
	})
	return stored, err
}

func (r MessageStreams) List(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, after uint64, limit int) ([]messagemodel.Message, error) {
	var result []messagemodel.Message
	err := r.s.view(ctx, func(readCtx context.Context) error {
		owner, err := r.owner(readCtx, sessionID, agentID)
		if err != nil {
			return err
		}
		stream, err := r.values(readCtx, owner, after, limit, false)
		result = stream.Messages
		return err
	})
	return result, err
}

func (r MessageStreams) LoadMessages(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, limit int) (messagemodel.Stream, error) {
	var result messagemodel.Stream
	err := r.s.view(ctx, func(readCtx context.Context) error {
		owner, err := r.owner(readCtx, sessionID, agentID)
		if err != nil {
			return err
		}
		result, err = r.values(readCtx, owner, 0, limit, true)
		return err
	})
	return result, err
}

func (r MessageStreams) Writer(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID) (toolcall.Writer, error) {
	owner, err := r.owner(ctx, sessionID, agentID)
	if err != nil {
		return nil, err
	}
	return MessageWriter{streams: r, owner: owner}, nil
}
func (r MessageStreams) owner(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID) (agentmodel.Agent, error) {
	owner, err := (AgentRepository{r.s}).Get(ctx, agentID)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	if owner.SessionID != sessionID {
		return agentmodel.Agent{}, fault.ErrNotFound
	}
	return owner, nil
}

type MessageWriter struct {
	streams MessageStreams
	owner   agentmodel.Agent
}

func (s *Store) appendMessage(ctx context.Context, owner agentmodel.Agent, data messagemodel.Message) (stored messagemodel.Message, inserted bool, err error) {
	if owner.SessionID == "" {
		return stored, false, errors.New("message session is required")
	}
	err = s.InTx(ctx, func(txCtx context.Context) error {
		scope, ownerID := owner.SessionID.String(), appendOwner(owner)
		prior, err := s.payload(txCtx, messageEntityKey(scope, ownerID, data.ID))
		if err == nil {
			var record messageRecord
			if err := decodeJSON(prior, &record); err != nil {
				return err
			}
			candidate := data
			candidate.Sequence, candidate.CreatedAt = record.Data.Sequence, record.Data.CreatedAt
			if record.AgentID != owner.ID || !same(record.Data, candidate) {
				return fault.ErrRequestConflict
			}
			stored = record.Data
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		tx := s.tx(txCtx)
		seq := s.sequences[scope]
		if len(tx.events) > 0 {
			seq = tx.nextSequence
		}
		data.Sequence = seq + 1
		if data.CreatedAt.IsZero() {
			data.CreatedAt = time.Now().UTC()
		}
		record := messageRecord{
			OwnerID: ownerID,
			AgentID: owner.ID,
			Data:    data,
		}
		if err := record.Validate(); err != nil {
			return err
		}
		if err := save(txCtx, s, scope, "message", messageKey(ownerID, data.ID), "message.appended", record); err != nil {
			return err
		}
		stored, inserted = data, true
		return nil
	})
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}

// AppendToolResult 原子追加回执并累计输出字节；普通 Append 不修改预算。
func (w MessageWriter) AppendToolResult(ctx context.Context, data messagemodel.Message) error {
	if data.Role != messagemodel.RoleTool || data.TaskID == "" {
		return errors.New("tool receipt must include task identity")
	}
	return w.streams.s.InTx(ctx, func(txCtx context.Context) error {
		stored, inserted, err := w.streams.s.appendMessage(txCtx, w.owner, data)
		if err != nil || !inserted {
			return err
		}
		task, err := (TaskRepository{w.streams.s}).Get(txCtx, identity.TaskID(stored.TaskID))
		if err != nil {
			return err
		}
		if task.AgentID != w.owner.ID || task.SessionID != w.owner.SessionID {
			return fault.ErrRequestConflict
		}
		if task.Active() {
			for _, block := range stored.Blocks {
				task.Progress.OutputBytes += int64(len(block.Text))
			}
			return (TaskRepository{w.streams.s}).Save(txCtx, task)
		}
		return nil
	})
}

func (r MessageStreams) values(ctx context.Context, owner agentmodel.Agent, after uint64, limit int, tail bool) (messagemodel.Stream, error) {
	scope, ownerID := owner.SessionID.String(), appendOwner(owner)
	if tx := r.s.tx(ctx); tx != nil && tx.deletedScope && tx.scope == scope {
		return messagemodel.Stream{}, fault.ErrNotFound
	}
	index, err := r.s.messageIndex(ctx, scope)
	if err != nil {
		return messagemodel.Stream{}, err
	}
	refs := index.streams[ownerID]
	// 仅复制本次查询的引用，不修改共享缓存；事务消息由变更集补入。
	selected := make([]messageRef, 0)
	for _, ref := range refs {
		if ref.sequence > after {
			selected = append(selected, ref)
		}
	}
	if tx := r.s.tx(ctx); tx != nil {
		for key, change := range tx.changes {
			if key.collection != "message" || change.entry.location.Scope != scope {
				continue
			}
			var record messageRecord
			if err := decodeJSON(change.event.Payload, &record); err != nil {
				return messagemodel.Stream{}, err
			}
			if record.OwnerID == ownerID && record.Data.Sequence > after {
				selected = append(selected, messageRef{id: record.Data.ID, sequence: record.Data.Sequence})
			}
		}
	}
	sorted(selected, func(a, b messageRef) int { return cmp.Compare(a.sequence, b.sequence) })
	var boundary uint64
	if len(selected) > 0 {
		boundary = selected[len(selected)-1].sequence
	}
	if limit > 0 {
		if tail {
			selected = selected[max(0, len(selected)-limit):]
		} else {
			selected = selected[:min(len(selected), limit)]
		}
	}
	result := make([]messagemodel.Message, 0, len(selected))
	reader := make(recordReader)
	for _, ref := range selected {
		key := messageEntityKey(scope, ownerID, ref.id)
		var data []byte
		if change, ok := r.s.tx(ctx).changes[key]; ok {
			data = change.event.Payload
		} else {
			data, err = r.s.readRef(key, ref.location, reader)
			if err != nil {
				return messagemodel.Stream{}, err
			}
		}
		var record messageRecord
		if err := decodeJSON(data, &record); err != nil {
			return messagemodel.Stream{}, err
		}
		result = append(result, record.Data)
	}
	return messagemodel.Stream{Messages: result, SequenceBoundary: boundary}, nil
}

// Validate 在消息写入或磁盘读取入口检查消息结构和流身份。
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

// validateMessageData 在消息写入和磁盘读取入口检查身份、角色及内容结构。
func validateMessageData(m messagemodel.Message) error {
	if m.ID == "" || m.Sequence == 0 || m.CreatedAt.IsZero() {
		return errors.New("message identity, sequence and creation time are required")
	}
	for _, value := range []string{
		m.ID,
		m.RequestID,
		m.HandoffID,
		m.TaskID,
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

var _ messagemodel.Streams = MessageStreams{}
var _ toolcall.Writer = MessageWriter{}
