package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"time"
)

// appendLocked 先更新内存状态，再按顺序持久化；失败保留待写记录供重试或重启恢复。
// 调用方的条件检查、序号分配和本次写入必须处于同一次持锁操作中。
func (s *Store) appendLocked(ctx context.Context, scope, collection, id, kind string, object any) error {
	if err := s.flushLocked(ctx); err != nil {
		return err
	}
	if scope != "" && collection != "session" && collection != "usage" && collection != "agent_execution_timing" {
		session, err := getLocked[sessionmodel.Session](s, "session", scope)
		if err != nil {
			return err
		}
		if session.State != sessionmodel.SessionActive {
			return errorcode.ErrRequestConflict
		}
	}
	event := jsonl.Event{
		Sequence:   s.sequences[scope] + 1,
		ID:         fmt.Sprintf("event:%d", s.sequences[scope]+1),
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
		_, entry, err = s.describe(scope, event, object)
		if err != nil {
			return err
		}
		if prior, ok := s.entry(eventKey(scope, event)); ok && prior.scope == scope {
			payload, err := s.payload(eventKey(scope, event))
			if err != nil {
				return err
			}
			if bytes.Equal(payload, event.Payload) {
				return nil
			}
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
	if collection == "entry" {
		s.applyEvent(scope, event, entry)
		s.pending = append(s.pending, pendingEvent{scope: scope, projectID: projectID, event: event})
		return s.flushLocked(ctx)
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
			return agentmodel.ErrExecuting
		}
		return nil
	}
	key := eventKey(scope, event)
	if prior, ok := s.entry(key); ok {
		if prior.scope != scope {
			return errorcode.ErrRequestConflict
		}
		payload, err := s.payload(key)
		if err != nil {
			return err
		}
		if err := immutable(key.collection, payload, event.Payload); err != nil {
			return err
		}
	} else if key.collection == "session" && s.log.Sequence(scope) != 0 {
		return errors.New("session identity cannot be reused")
	}
	if scope != "" && key.collection != "session" {
		if _, ok := s.entry(entityKey{"session", scope}); !ok {
			return errorcode.ErrNotFound
		}
	}
	claimed := make(map[lookupKey]bool, len(entry.claims))
	for _, claim := range entry.claims {
		if claimed[claim] {
			return errorcode.ErrRequestConflict
		}
		claimed[claim] = true
		if old, exists := s.state.unique[claim]; exists && old != key {
			return errorcode.ErrRequestConflict
		}
	}
	return s.validateRelations(key, entry)
}

func (s *Store) applyEvent(scope string, event jsonl.Event, entry stateEntry) {
	s.sequences[scope] = event.Sequence
	if event.Type == "session.deleted" {
		// 在移除 Session 索引前释放所属 Agent 的历史，避免留下不可再访问的正文。
		for key := range s.state.groups[lookupKey{kind: "entries-session", owner: scope}] {
			delete(s.histories, s.state.entries[key].record.AgentID)
		}
		s.state.removeScope(scope)
		return
	}
	key := eventKey(scope, event)
	s.state.remove(key)
	if entry.record != nil {
		entry.payload = nil
	}
	s.state.add(key, entry)
	if entry.record != nil {
		s.history(entry.record.AgentID).Append(entry.record.Data)
	}
}

type pendingEvent struct {
	scope     string
	projectID string
	event     jsonl.Event
}

// flushLocked 仅负责追加待写事件，不重新执行模型或工具，不回滚已接收的内存事实。
func (s *Store) flushLocked(ctx context.Context) error {
	for len(s.pending) > 0 {
		next := s.pending[0]
		if err := s.log.Append(ctx, next.scope, next.projectID, next.event); err != nil {
			return err
		}
		s.pending[0] = pendingEvent{}
		s.pending = s.pending[1:]
	}
	return nil
}

func (s *Store) history(agentID identity.AgentID) *agentmodel.History {
	h := s.histories[agentID]
	if h == nil {
		h = &agentmodel.History{}
		s.histories[agentID] = h
	}
	return h
}
