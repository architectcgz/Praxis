package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/repository"
)

type change struct {
	event Event
	entry indexEntry
}

type transaction struct {
	store          *Store
	active         bool
	readOnly       bool
	scope          string
	nextSequence   uint64
	events         []Event
	changes        map[entityKey]change
	deletedScope   bool
	receiptChanges map[receiptKey]*repository.MissingToolResult
	// 启动只为正在扫描的一个 Session 临时保存消息位置，扫描结束即释放。
	replayMessages *sessionMessages
}

type transactionKey struct{}

func (s *Store) tx(ctx context.Context) *transaction {
	if ctx == nil {
		return nil
	}
	tx, _ := ctx.Value(transactionKey{}).(*transaction)
	if tx != nil && tx.store == s {
		return tx
	}
	return nil
}

func (s *Store) newTransaction() *transaction {
	return &transaction{store: s, active: true, changes: make(map[entityKey]change)}
}

func (s *Store) view(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		return errors.New("查询 Context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx := s.tx(ctx); tx != nil {
		if !tx.active {
			return errors.New("事务已经结束")
		}
		return fn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("日志已关闭")
	}
	if s.fault != nil {
		return s.fault
	}
	tx := &transaction{store: s, active: true, readOnly: true}
	defer func() { tx.active = false }()
	return fn(context.WithValue(ctx, transactionKey{}, tx))
}

// InTx 只保留本次变更；嵌套调用共享事务，最终关联约束在整笔事务完成后验证。
func (s *Store) InTx(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil || fn == nil {
		return errors.New("事务 Context 和回调不能为空")
	}
	if tx := s.tx(ctx); tx != nil {
		if !tx.active {
			return errors.New("事务已经结束")
		}
		if tx.readOnly {
			return errors.New("只读查询不能开启写事务")
		}
		return fn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("日志已关闭")
	}
	if s.fault != nil {
		return s.fault
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := s.newTransaction()
	defer func() { tx.active = false }()
	txCtx := context.WithValue(ctx, transactionKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if len(tx.events) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validateTransaction(txCtx); err != nil {
		return err
	}
	encoded, err := json.Marshal(batch{Events: tx.events})
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	offset := s.lengths[tx.scope]
	if err := s.append(tx.scope, encoded); err != nil {
		return err
	}
	s.publish(tx, offset, int64(len(encoded)))
	return nil
}

func (s *Store) change(ctx context.Context, scope, collection, id, kind string, data json.RawMessage) error {
	tx := s.tx(ctx)
	if tx == nil || !tx.active || tx.readOnly {
		return errors.New("变更必须位于活动事务内")
	}
	if len(tx.events) == 0 {
		tx.scope, tx.nextSequence = scope, s.sequences[scope]
	}
	if tx.scope != scope {
		return errors.New("一次事务不能写入多个日志文件")
	}
	event := Event{
		Sequence:   tx.nextSequence + 1,
		ID:         fmt.Sprintf("event:%d", tx.nextSequence+1),
		Type:       kind,
		Collection: collection,
		Key:        id,
		CreatedAt:  time.Now().UTC(),
		Payload:    bytes.Clone(data),
	}
	return s.stage(ctx, event)
}

func (s *Store) stage(ctx context.Context, event Event) error {
	tx := s.tx(ctx)
	if tx.deletedScope || s.index.deleted[tx.scope] {
		return errors.New("已删除的 Session 不允许继续写入")
	}
	if _, err := s.path(tx.scope); err != nil {
		return err
	}
	if event.Sequence != tx.nextSequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
		return errors.New("事件身份或序号非法")
	}
	if event.Type == "session.deleted" {
		if tx.scope == "" || event.Collection != "session" || event.Key != tx.scope || len(event.Payload) > 0 {
			return errors.New("非法删除事件")
		}
		tx.deletedScope = true
	} else {
		key, entry, err := describe(tx.scope, event)
		if err != nil {
			return err
		}
		prior, err := s.payload(ctx, key)
		if err == nil {
			if key.collection != "message" {
				old, _ := s.entry(ctx, key)
				if old.location.Scope != tx.scope {
					return contracts.ErrRequestConflict
				}
			}
			if err := immutable(event.Collection, prior, event.Payload); err != nil {
				return err
			}
			if bytes.Equal(prior, event.Payload) {
				return nil
			}
		} else if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		entry.location = recordRef{Scope: tx.scope, EventIndex: len(tx.events), Sequence: event.Sequence}
		tx.changes[key] = change{event: event, entry: entry}
	}
	tx.nextSequence = event.Sequence
	tx.events = append(tx.events, event)
	return nil
}
