package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/fault"
	"praxis/internal/infra/jsonl"
	"time"
)

// appendLocked 在 Store.mu 内校验并追加单个事实；日志同步成功前不修改 State。
// 调用方的条件检查、序号分配和本次写入必须处于同一次持锁操作中。
func (s *Store) appendLocked(ctx context.Context, scope, collection, id, kind string, object any) error {
	if scope != "" && collection != "session" && collection != "usage" && collection != "timing" {
		session, err := getLocked[sessionmodel.Session](s, "session", scope)
		if err != nil {
			return err
		}
		if session.State != sessionmodel.SessionActive {
			return fault.ErrRequestConflict
		}
	}
	event := jsonl.Event{
		Sequence:   s.log.Sequence(scope) + 1,
		ID:         fmt.Sprintf("event:%d", s.log.Sequence(scope)+1),
		Type:       kind,
		Collection: collection,
		Key:        id,
		CreatedAt:  time.Now().UTC(),
	}
	var entry stateEntry
	if kind != "session.deleted" {
		var err error
		event.Payload, err = json.Marshal(object)
		if err != nil {
			return err
		}
		_, entry, err = describe(scope, event, object)
		if err != nil {
			return err
		}
		if prior, ok := s.entry(eventKey(scope, event)); ok && prior.scope == scope && bytes.Equal(prior.payload, event.Payload) {
			return nil
		}
	}
	if err := s.validateEvent(scope, event, entry); err != nil {
		return fmt.Errorf("%s/%s: %w", collection, id, err)
	}
	projectID := s.log.Project(scope)
	if collection == "session" && kind != "session.deleted" {
		for _, group := range entry.groups {
			if group.kind == "sessions-project" {
				projectID = group.owner
			}
		}
	}
	if err := s.log.Append(ctx, scope, projectID, event); err != nil {
		return err
	}
	s.applyEvent(scope, event, entry)
	return nil
}

// validateEvent 只对照已提交事实检查单条候选事件，校验期间不修改 State。
func (s *Store) validateEvent(scope string, event jsonl.Event, entry stateEntry) error {
	if scope != "" && !jsonl.ValidScope(scope) {
		return errors.New("session ID is not a valid file name")
	}
	if event.Type == "session.deleted" {
		if scope == "" || event.Collection != "session" || event.Key != scope || len(event.Payload) != 0 {
			return errors.New("invalid deletion event")
		}
		if _, err := s.payload(entityKey{"session", scope}); err != nil {
			return err
		}
		if len(s.keys(lookupKey{kind: "active-session", owner: scope})) != 0 {
			return agentmodel.ErrAgentExecuting
		}
		return nil
	}
	key := eventKey(scope, event)
	if prior, ok := s.entry(key); ok {
		if prior.scope != scope {
			return fault.ErrRequestConflict
		}
		if err := immutable(key.collection, prior.payload, event.Payload); err != nil {
			return err
		}
	} else if key.collection == "session" && s.log.Sequence(scope) != 0 {
		return errors.New("session identity cannot be reused")
	}
	if scope != "" && key.collection != "session" {
		if _, ok := s.entry(entityKey{"session", scope}); !ok {
			return fault.ErrNotFound
		}
	}
	claimed := make(map[lookupKey]bool, len(entry.claims))
	for _, claim := range entry.claims {
		if claimed[claim] {
			return fault.ErrRequestConflict
		}
		claimed[claim] = true
		if old, exists := s.state.unique[claim]; exists && old != key {
			return fault.ErrRequestConflict
		}
	}
	return s.validateRelations(key, entry)
}

func (s *Store) applyEvent(scope string, event jsonl.Event, entry stateEntry) {
	if event.Type == "session.deleted" {
		s.state.removeScope(scope)
		return
	}
	key := eventKey(scope, event)
	s.state.remove(key)
	s.state.add(key, entry)
}
