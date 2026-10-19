package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"time"
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
	receiptChanges map[receiptKey]*messagemodel.MissingToolResult
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
		return errors.New("query context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx := s.tx(ctx); tx != nil {
		if !tx.active {
			return errors.New("transaction has ended")
		}
		return fn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("log is closed")
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
		return errors.New("transaction context and callback are required")
	}
	if tx := s.tx(ctx); tx != nil {
		if !tx.active {
			return errors.New("transaction has ended")
		}
		if tx.readOnly {
			return errors.New("a read-only query cannot start a write transaction")
		}
		return fn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("log is closed")
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
	if err := s.append(txCtx, tx.scope, encoded); err != nil {
		return err
	}
	s.publish(tx, offset, int64(len(encoded)))
	return nil
}

func (s *Store) change(ctx context.Context, scope, collection, id, kind string, data json.RawMessage, object any) error {
	tx := s.tx(ctx)
	if tx == nil || !tx.active || tx.readOnly {
		return errors.New("changes require an active transaction")
	}
	if len(tx.events) == 0 {
		tx.scope, tx.nextSequence = scope, s.sequences[scope]
	}
	if tx.scope != scope {
		return errors.New("a transaction cannot write to multiple log files")
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
	var entry indexEntry
	if event.Type != "session.deleted" {
		var err error
		_, entry, err = describe(scope, event, object)
		if err != nil {
			return err
		}
	}
	return s.stage(ctx, event, entry)
}

func (s *Store) stage(ctx context.Context, event Event, entry indexEntry) error {
	tx := s.tx(ctx)
	if tx.deletedScope || s.index.deleted[tx.scope] {
		return errors.New("cannot write to a deleted session")
	}
	if tx.scope != "" && !validScope(tx.scope) {
		return errors.New("session ID is not a valid file name")
	}
	if event.Sequence != tx.nextSequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
		return errors.New("invalid event identity or sequence")
	}
	if event.Type == "session.deleted" {
		if tx.scope == "" || event.Collection != "session" || event.Key != tx.scope || len(event.Payload) > 0 {
			return errors.New("invalid deletion event")
		}
		tx.deletedScope = true
	} else {
		key := eventKey(tx.scope, event)
		prior, err := s.payload(ctx, key)
		if err == nil {
			if key.collection != "message" {
				old, _ := s.entry(ctx, key)
				if old.location.Scope != tx.scope {
					return fault.ErrRequestConflict
				}
			}
			if err := immutable(event.Collection, prior, event.Payload); err != nil {
				return err
			}
			if bytes.Equal(prior, event.Payload) {
				return nil
			}
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		entry.location = recordRef{Scope: tx.scope, EventIndex: len(tx.events), Sequence: event.Sequence}
		tx.changes[key] = change{event: event, entry: entry}
	}
	tx.nextSequence = event.Sequence
	tx.events = append(tx.events, event)
	return nil
}
