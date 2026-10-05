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
	"praxis/internal/repository"
	"praxis/internal/timing"
)

type messageRecord struct {
	OwnerID string                   `json:"owner_id,omitempty"`
	Data    sessionmodel.MessageData `json:"message"`
}
type SessionMessageRepository struct{ s *Store }
type AgentMessageRepository struct{ s *Store }

func messageKey(owner, id string) string { return fmt.Sprintf("%d:%s%s", len(owner), owner, id) }

// projectionKey 为消息增加仅存在于内存中的 Session 命名空间。
func projectionKey(scope, collection, id string) objectKey {
	if collection == "message" {
		id = scope + "\x00" + id
	}
	return objectKey{collection, id}
}
func (s *Store) appendMessage(ctx context.Context, scope, owner string, data sessionmodel.MessageData) (sessionmodel.MessageData, error) {
	var result sessionmodel.MessageData
	err := s.InTx(ctx, func(ctx context.Context) error {
		existingObject, exists := s.tx(ctx).objects[projectionKey(scope, "message", messageKey(owner, data.ID))]
		if exists {
			existing, err := decode[messageRecord]("message", existingObject)
			if err != nil {
				return err
			}
			candidate := data
			candidate.Sequence = existing.Data.Sequence
			candidate.CreatedAt = existing.Data.CreatedAt
			if !same(existing.Data, candidate) {
				return contracts.ErrRequestConflict
			}
			result = existing.Data
			return nil
		}
		data.Sequence = s.tx(ctx).sequences[scope] + 1
		if data.CreatedAt.IsZero() {
			data.CreatedAt = time.Now().UTC()
		}
		if e := data.Validate(); e != nil {
			return e
		}
		if e := save(ctx, s, scope, "message", messageKey(owner, data.ID), "message.appended", messageRecord{owner, data}); e != nil {
			return e
		}
		result = data
		return nil
	})
	return result, err
}
func (s *Store) messages(ctx context.Context, scope, owner string, turn contracts.TurnID, after uint64, limit int) ([]sessionmodel.MessageData, error) {
	result := make([]sessionmodel.MessageData, 0)
	err := s.view(ctx, func(objects map[objectKey]object) error {
		for key, value := range objects {
			if key.collection != "message" || value.scope != scope {
				continue
			}
			record, e := decode[messageRecord]("message", value)
			if e != nil {
				return e
			}
			if record.OwnerID == owner && record.Data.Sequence > after && (turn == "" || record.Data.TurnID == turn.String()) {
				result = append(result, record.Data)
			}
		}
		return nil
	})
	sorted(result, func(a, b sessionmodel.MessageData) int { return cmp.Compare(a.Sequence, b.Sequence) })
	if limit > 0 {
		result = result[:min(len(result), limit)]
	}
	return result, err
}

// Append 幂等追加记录，身份相同但内容不一致时返回冲突。
func (r SessionMessageRepository) Append(ctx context.Context, v sessionmodel.SessionMessage) (sessionmodel.SessionMessage, error) {
	if v.SessionID == "" {
		return v, errors.New("消息 Session 不能为空")
	}
	data, e := r.s.appendMessage(ctx, v.SessionID.String(), "", v.Data)
	return sessionmodel.SessionMessage{SessionID: v.SessionID, Data: data}, e
}

// Append 幂等追加记录，身份相同但内容不一致时返回冲突。
func (r AgentMessageRepository) Append(ctx context.Context, v agentmodel.AgentMessage) (agentmodel.AgentMessage, error) {
	a, e := (AgentRepository{r.s}).Get(ctx, v.AgentID)
	if e != nil {
		return v, e
	}
	data, e := r.s.appendMessage(ctx, a.SessionID.String(), a.ID.String(), v.Data)
	return agentmodel.AgentMessage{AgentID: v.AgentID, Data: data}, e
}

// List 返回指定归属及游标之后的记录副本，非正数 limit 表示不限制。
func (r SessionMessageRepository) List(ctx context.Context, id contracts.SessionID, after uint64, limit int) ([]sessionmodel.SessionMessage, error) {
	v, e := r.s.messages(ctx, id.String(), "", "", after, limit)
	result := make([]sessionmodel.SessionMessage, len(v))
	for i, data := range v {
		result[i] = sessionmodel.SessionMessage{SessionID: id, Data: data}
	}
	return result, e
}

// ListByTurn 返回指定 Turn 的有序记录。
func (r SessionMessageRepository) ListByTurn(ctx context.Context, id contracts.SessionID, turn contracts.TurnID) ([]sessionmodel.SessionMessage, error) {
	v, e := r.s.messages(ctx, id.String(), "", turn, 0, 0)
	result := make([]sessionmodel.SessionMessage, len(v))
	for i, data := range v {
		result[i] = sessionmodel.SessionMessage{SessionID: id, Data: data}
	}
	return result, e
}

// List 返回指定归属及游标之后的记录副本，非正数 limit 表示不限制。
func (r AgentMessageRepository) List(ctx context.Context, id contracts.AgentID, after uint64, limit int) ([]agentmodel.AgentMessage, error) {
	a, e := (AgentRepository{r.s}).Get(ctx, id)
	if e != nil {
		return nil, e
	}
	v, e := r.s.messages(ctx, a.SessionID.String(), id.String(), "", after, limit)
	result := make([]agentmodel.AgentMessage, len(v))
	for i, data := range v {
		result[i] = agentmodel.AgentMessage{AgentID: id, Data: data}
	}
	return result, e
}

// ListByTurn 返回指定 Turn 的有序记录。
func (r AgentMessageRepository) ListByTurn(ctx context.Context, id contracts.AgentID, turn contracts.TurnID) ([]agentmodel.AgentMessage, error) {
	a, e := (AgentRepository{r.s}).Get(ctx, id)
	if e != nil {
		return nil, e
	}
	v, e := r.s.messages(ctx, a.SessionID.String(), id.String(), turn, 0, 0)
	result := make([]agentmodel.AgentMessage, len(v))
	for i, data := range v {
		result[i] = agentmodel.AgentMessage{AgentID: id, Data: data}
	}
	return result, e
}

// LatestSequence 返回当前消息流的最大序号，没有消息时返回零。
func (r SessionMessageRepository) LatestSequence(ctx context.Context, id contracts.SessionID) (uint64, error) {
	v, e := r.List(ctx, id, 0, 0)
	if len(v) == 0 {
		return 0, e
	}
	return v[len(v)-1].Data.Sequence, e
}

// LatestSequence 返回当前消息流的最大序号，没有消息时返回零。
func (r AgentMessageRepository) LatestSequence(ctx context.Context, id contracts.AgentID) (uint64, error) {
	v, e := r.List(ctx, id, 0, 0)
	if len(v) == 0 {
		return 0, e
	}
	return v[len(v)-1].Data.Sequence, e
}

// TimingStore 将计时更新放入所属 Session 日志，不影响主业务事务。
type TimingStore struct{ Store *Store }

// Save 拒绝旧版本及终态回退，重复保存相同版本时要求内容一致。
func (r TimingStore) Save(ctx context.Context, v timing.Record) error {
	return r.Store.InTx(ctx, func(ctx context.Context) error {
		old, e := get[timing.Record](ctx, r.Store, "timing", v.ID)
		if e == nil {
			if v.Revision < old.Revision {
				return errors.New("拒绝过期计时版本")
			}
			if v.Revision == old.Revision {
				if same(old, v) {
					return nil
				}
				return contracts.ErrRequestConflict
			}
			if old.Status != timing.Running || old.Operation != v.Operation || old.ParentID != v.ParentID || !old.StartedAt.Equal(v.StartedAt) {
				return contracts.ErrRequestConflict
			}
		} else if !errors.Is(e, contracts.ErrNotFound) {
			return e
		}
		return save(ctx, r.Store, v.SessionID, "timing", v.ID, "timing.saved", v)
	})
}

// ListAgent 返回指定 Agent 按开始时间倒序的计时记录。
func (r TimingStore) ListAgent(ctx context.Context, id string, limit int) ([]timing.Record, error) {
	v, e := list[timing.Record](ctx, r.Store, "timing", func(v timing.Record) bool { return v.AgentID == id })
	return capped(sorted(v, func(a, b timing.Record) int { return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID)) }), limit), e
}

// RecoverInterrupted 将未结束计时收敛为未知时长，不捏造完成时间。
func (r TimingStore) RecoverInterrupted(ctx context.Context) error {
	values, e := list[timing.Record](ctx, r.Store, "timing", func(v timing.Record) bool { return v.Status == timing.Running })
	if e != nil {
		return e
	}
	for _, v := range values {
		v.Status = timing.Interrupted
		v.Revision++
		if e = r.Save(ctx, v); e != nil {
			return e
		}
	}
	return nil
}

var _ repository.SessionMessageRepository = SessionMessageRepository{}
var _ repository.AgentMessageRepository = AgentMessageRepository{}
var _ timing.Store = TimingStore{}
