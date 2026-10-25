package storage

import (
	"bytes"
	"context"
	"errors"
	"praxis/internal/infra/jsonl"
)

func (s *Store) replay(ctx context.Context, scope jsonl.Scope) error {
	// Open 尚未发布 Store，恢复过程独占 State；删除后不允许出现后续事件。
	deleted := false
	visit := func(event jsonl.Event) error {
		if deleted {
			return errors.New("events follow a deleted session")
		}
		var entry stateEntry
		if event.Type != "session.deleted" {
			var err error
			entry, err = s.restoreStateEntry(scope.ID, event)
			if err != nil {
				return err
			}
			if _, ok := s.entry(eventKey(scope.ID, event)); ok {
				payload, err := s.payload(eventKey(scope.ID, event))
				if err != nil {
					return err
				}
				if bytes.Equal(payload, event.Payload) {
					if event.Collection == "agent_execution_timing" {
						s.sequences[scope.ID] = event.Sequence
						return nil
					}
					return errors.New("redundant or duplicate event")
				}
			}
		}
		if event.Collection == "session" && event.Type != "session.deleted" {
			for _, group := range entry.groups {
				if group.kind == "sessions-project" && group.owner != scope.ProjectID {
					return errors.New("session project does not match log directory")
				}
			}
		}
		if err := s.validateEvent(scope.ID, event, entry); err != nil {
			return err
		}
		s.applyEvent(scope.ID, event, entry)
		deleted = event.Type == "session.deleted"
		return nil
	}
	return s.log.With(ctx, func(log *jsonl.Access) error {
		if scope.ID == "" {
			return log.ReadProject(visit)
		}
		return log.ReadSession(scope.ID, visit)
	})
}
