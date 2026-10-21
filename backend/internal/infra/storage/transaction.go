package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/fault"
	"praxis/internal/infra/jsonl"
	"time"
)

type change struct {
	event jsonl.Event
	entry stateEntry
}

type transaction struct {
	store        *Store
	active       bool
	readOnly     bool
	scope        string
	nextSequence uint64
	events       []jsonl.Event
	changes      map[entityKey]change
	deletedScope bool
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
	if err := s.log.Check(); err != nil {
		return err
	}
	tx := &transaction{store: s, active: true, readOnly: true}
	defer func() { tx.active = false }()
	return fn(context.WithValue(ctx, transactionKey{}, tx))
}

// InTx 暂存本次变更；校验成功后先提交日志，再原子发布 State。
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
	if err := s.log.Check(); err != nil {
		return err
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
	if err := s.prepareCommit(txCtx); err != nil {
		return err
	}
	if err := s.log.Append(txCtx, tx.scope, s.project(txCtx, tx.scope), jsonl.Commit{Events: tx.events}); err != nil {
		return err
	}
	s.publish(tx)
	return nil
}

func (s *Store) change(ctx context.Context, scope, collection, id, kind string, data json.RawMessage, object any) error {
	tx := s.tx(ctx)
	if tx == nil || !tx.active || tx.readOnly {
		return errors.New("changes require an active transaction")
	}
	if len(tx.events) == 0 {
		tx.scope, tx.nextSequence = scope, s.log.Sequence(scope)
	}
	if tx.scope != scope {
		return errors.New("a transaction cannot write to multiple log files")
	}
	event := jsonl.Event{
		Sequence:   tx.nextSequence + 1,
		ID:         fmt.Sprintf("event:%d", tx.nextSequence+1),
		Type:       kind,
		Collection: collection,
		Key:        id,
		CreatedAt:  time.Now().UTC(),
		Payload:    bytes.Clone(data),
	}
	var entry stateEntry
	if event.Type != "session.deleted" {
		var err error
		_, entry, err = describe(scope, event, object)
		if err != nil {
			return err
		}
	}
	return s.stage(ctx, event, entry)
}

func (s *Store) stage(ctx context.Context, event jsonl.Event, entry stateEntry) error {
	tx := s.tx(ctx)
	if tx.deletedScope {
		return errors.New("cannot write to a deleted session")
	}
	if tx.scope != "" && !jsonl.ValidScope(tx.scope) {
		return errors.New("session ID is not a valid file name")
	}
	if tx.nextSequence == ^uint64(0) || event.Sequence != tx.nextSequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
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
				if old.scope != tx.scope {
					return fault.ErrRequestConflict
				}
			}
			if bytes.Equal(prior, event.Payload) {
				return nil
			}
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		entry.scope, entry.eventSequence = tx.scope, event.Sequence
		tx.changes[key] = change{event: event, entry: entry}
	}
	tx.nextSequence = event.Sequence
	tx.events = append(tx.events, event)
	return nil
}
