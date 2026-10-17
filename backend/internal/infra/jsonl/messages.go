package jsonl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	toolexecution "praxis/internal/core/tool_execution"
	"praxis/internal/repository"
)

type messageRecord struct {
	OwnerID string                   `json:"owner_id,omitempty"`
	AgentID contracts.AgentID        `json:"agent_id"`
	Data    sessionmodel.MessageData `json:"message"`
	Outcome *toolexecution.Outcome   `json:"outcome,omitempty"`
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

func (r MessageStreams) Append(ctx context.Context, owner agentmodel.Agent, data sessionmodel.MessageData) (sessionmodel.MessageData, error) {
	stored, _, err := r.s.appendMessage(ctx, owner, data, nil)
	return stored, err
}

func (r MessageStreams) List(ctx context.Context, owner agentmodel.Agent, after uint64, limit int) ([]sessionmodel.MessageData, error) {
	var result []sessionmodel.MessageData
	err := r.s.view(ctx, func(readCtx context.Context) error {
		if _, err := r.owner(readCtx, owner.SessionID, owner.ID); err != nil {
			return err
		}
		stream, err := r.values(readCtx, owner, after, limit, false)
		result = stream.Messages
		return err
	})
	return result, err
}

func (r MessageStreams) LoadMessages(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID, limit int) (repository.MessageStream, error) {
	var result repository.MessageStream
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

func (r MessageStreams) Writer(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID) (repository.MessageWriter, error) {
	owner, err := r.owner(ctx, sessionID, agentID)
	if err != nil {
		return nil, err
	}
	return MessageWriter{streams: r, owner: owner}, nil
}
func (r MessageStreams) owner(ctx context.Context, sessionID contracts.SessionID, agentID contracts.AgentID) (agentmodel.Agent, error) {
	owner, err := (AgentRepository{r.s}).Get(ctx, agentID)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	if owner.SessionID != sessionID {
		return agentmodel.Agent{}, contracts.ErrNotFound
	}
	return owner, nil
}

type MessageWriter struct {
	streams MessageStreams
	owner   agentmodel.Agent
}

func (s *Store) appendMessage(ctx context.Context, owner agentmodel.Agent, data sessionmodel.MessageData, outcome *toolexecution.Outcome) (stored sessionmodel.MessageData, inserted bool, err error) {
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
			if record.AgentID != owner.ID || !same(record.Data, candidate) || !same(record.Outcome, outcome) {
				return contracts.ErrRequestConflict
			}
			stored = record.Data
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
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
		if err := save(txCtx, s, scope, "message", messageKey(ownerID, data.ID), "message.appended", messageRecord{
			OwnerID: ownerID,
			AgentID: owner.ID,
			Data:    data,
			Outcome: outcome,
		}); err != nil {
			return err
		}
		stored, inserted = data, true
		return nil
	})
	if err != nil {
		return sessionmodel.MessageData{}, false, err
	}
	return stored, inserted, nil
}

// AppendToolResult 原子追加回执并累计输出字节；普通 Append 不修改预算。
func (w MessageWriter) AppendToolResult(ctx context.Context, data sessionmodel.MessageData) error {
	if data.Role != sessionmodel.RoleTool || data.TaskID == "" {
		return errors.New("tool receipt must include task identity")
	}
	return w.streams.s.InTx(ctx, func(txCtx context.Context) error {
		stored, inserted, err := w.streams.s.appendMessage(txCtx, w.owner, data, nil)
		if err != nil || !inserted {
			return err
		}
		task, err := (TaskRepository{w.streams.s}).Get(txCtx, contracts.TaskID(stored.TaskID))
		if err != nil {
			return err
		}
		if task.AgentID != w.owner.ID || task.SessionID != w.owner.SessionID {
			return contracts.ErrRequestConflict
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

func (r MessageStreams) values(ctx context.Context, owner agentmodel.Agent, after uint64, limit int, tail bool) (repository.MessageStream, error) {
	scope, ownerID := owner.SessionID.String(), appendOwner(owner)
	if tx := r.s.tx(ctx); tx != nil && tx.deletedScope && tx.scope == scope {
		return repository.MessageStream{}, contracts.ErrNotFound
	}
	index, err := r.s.messageIndex(ctx, scope)
	if err != nil {
		return repository.MessageStream{}, err
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
				return repository.MessageStream{}, err
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
	result := make([]sessionmodel.MessageData, 0, len(selected))
	reader := make(recordReader)
	for _, ref := range selected {
		key := messageEntityKey(scope, ownerID, ref.id)
		var data []byte
		if change, ok := r.s.tx(ctx).changes[key]; ok {
			data = change.event.Payload
		} else {
			data, err = r.s.readRef(key, ref.location, reader)
			if err != nil {
				return repository.MessageStream{}, err
			}
		}
		var record messageRecord
		if err := decodeJSON(data, &record); err != nil {
			return repository.MessageStream{}, err
		}
		result = append(result, record.Data)
	}
	return repository.MessageStream{Messages: result, SequenceBoundary: boundary}, nil
}

var _ repository.MessageStreams = MessageStreams{}
var _ repository.MessageWriter = MessageWriter{}
