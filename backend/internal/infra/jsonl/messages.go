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

// MessageStreams 按 Agent 可见范围读写消息流；Session 流与私有流共用同一份日志键位。
type MessageStreams struct{ s *Store }

// appendOwner 返回 Agent 可见流在日志中的 owner 键；空值表示 Session 流。
func appendOwner(owner agentmodel.Agent) string {
	if owner.CanReadSessionContext() {
		return ""
	}
	return owner.ID.String()
}

// Append 幂等追加消息到 Agent 可见流，归属缺失时拒绝写入。
func (r MessageStreams) Append(
	ctx context.Context,
	owner agentmodel.Agent,
	data sessionmodel.MessageData,
) (sessionmodel.MessageData, error) {
	if owner.SessionID == "" {
		return data, errors.New("消息 Session 不能为空")
	}
	return r.s.appendMessage(ctx, owner.SessionID.String(), appendOwner(owner), data)
}

// List 返回 Agent 可见流中游标之后的记录副本；非正数 limit 表示不限制。
func (r MessageStreams) List(
	ctx context.Context,
	owner agentmodel.Agent,
	after uint64,
	limit int,
) ([]sessionmodel.MessageData, error) {
	// 已配对流直接读取现有投影，避免为普通历史查询复制事务索引。
	if r.s.tx(ctx) == nil {
		needsRepair := false
		var values []sessionmodel.MessageData
		err := r.s.view(ctx, func(objects map[objectKey]object) error {
			for key := range r.s.pending {
				if key.scope == owner.SessionID.String() && key.owner == appendOwner(owner) {
					needsRepair = true
					break
				}
			}
			if needsRepair {
				return nil
			}
			var err error
			values, err = messageValues(objects, owner.SessionID.String(), appendOwner(owner), after, limit)
			return err
		})
		if err != nil || !needsRepair {
			return values, err
		}
	}

	var values []sessionmodel.MessageData
	err := r.s.InTx(ctx, func(txCtx context.Context) error {
		if err := r.ensureToolReceipts(txCtx, owner); err != nil {
			return err
		}
		var err error
		values, err = r.s.messages(txCtx, owner.SessionID.String(), appendOwner(owner), after, limit)
		return err
	})
	return values, err
}

// LoadMessages 在一致性边界内读取 Agent 可见的消息流及其序号边界。
func (r MessageStreams) LoadMessages(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	limit int,
) (repository.MessageStream, error) {
	owner, err := r.owner(ctx, sessionID, agentID)
	if err != nil {
		return repository.MessageStream{}, err
	}
	values, err := r.List(ctx, owner, 0, 0)
	if err != nil {
		return repository.MessageStream{}, err
	}
	stream := repository.MessageStream{Messages: values}
	if len(values) > 0 {
		stream.SequenceBoundary = values[len(values)-1].Sequence
	}
	if limit > 0 {
		stream.Messages = values[max(0, len(values)-limit):]
	}
	return stream, nil

}

// Resolve 为指定执行主体解析绑定到其可见流的写入器；归属不匹配时返回 ErrNotFound。
func (r MessageStreams) Resolve(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
) (MessageWriter, error) {
	owner, err := r.owner(ctx, sessionID, agentID)
	if err != nil {
		return MessageWriter{}, err
	}
	return MessageWriter{streams: r, owner: owner}, nil
}

func (r MessageStreams) owner(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
) (agentmodel.Agent, error) {
	if ctx == nil {
		return agentmodel.Agent{}, errors.New("消息流上下文不能为空")
	}
	owner, err := (AgentRepository{r.s}).Get(ctx, agentID)
	if err != nil {
		return agentmodel.Agent{}, err
	}
	if owner.SessionID != sessionID {
		return agentmodel.Agent{}, contracts.ErrNotFound
	}
	return owner, nil
}

// MessageWriter 只写已解析归属的消息流；方法集满足 agent_runtime.MessageRecorder。
type MessageWriter struct {
	streams MessageStreams
	owner   agentmodel.Agent
}

// Append 幂等写入已解析归属的可见流。
func (w MessageWriter) Append(
	ctx context.Context,
	data sessionmodel.MessageData,
) (sessionmodel.MessageData, error) {
	return w.streams.Append(ctx, w.owner, data)
}

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
		if data.Role == sessionmodel.RoleTool && data.TaskID != "" {
			task, err := (TaskRepository{s}).Get(ctx, contracts.TaskID(data.TaskID))
			if err != nil {
				return err
			}
			if task.Active() {
				for _, block := range data.Blocks {
					task.Progress.OutputBytes += int64(len(block.Text))
				}
				if err := (TaskRepository{s}).Save(ctx, task); err != nil {
					return err
				}
			}
		}
		indexToolReceipts(s.tx(ctx).pending, s.tx(ctx).receipts, scope, messageRecord{OwnerID: owner, Data: data})
		result = data
		return nil
	})
	return result, err
}
func (s *Store) messages(ctx context.Context, scope, owner string, after uint64, limit int) ([]sessionmodel.MessageData, error) {
	var result []sessionmodel.MessageData
	err := s.view(ctx, func(objects map[objectKey]object) error {
		var err error
		result, err = messageValues(objects, scope, owner, after, limit)
		return err
	})
	return result, err
}
func messageValues(objects map[objectKey]object, scope, owner string, after uint64, limit int) ([]sessionmodel.MessageData, error) {
	result := make([]sessionmodel.MessageData, 0)
	for key, value := range objects {
		if key.collection != "message" || value.scope != scope {
			continue
		}
		record, err := decode[messageRecord]("message", value)
		if err != nil {
			return nil, err
		}
		if record.OwnerID == owner && record.Data.Sequence > after {
			result = append(result, record.Data)
		}
	}
	sorted(result, func(a, b sessionmodel.MessageData) int { return cmp.Compare(a.Sequence, b.Sequence) })
	if limit > 0 {
		result = result[:min(len(result), limit)]
	}
	return result, nil
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

var _ repository.MessageStreams = MessageStreams{}
var _ timing.Store = TimingStore{}
