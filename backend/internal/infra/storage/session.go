package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"strings"
	"time"
)

type SessionRepository struct{ storage *Storage }

func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error) {
	return get[sessionmodel.Session](ctx, r.storage, "session", id.String())
}

// Create 在内存锁内校验引用并接纳会话，文件创建与首条记录交由后台顺序保存。
func (r SessionRepository) Create(ctx context.Context, value sessionmodel.Session) error {
	return withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		id := value.ID.String()
		if state.sequences[id] != 0 {
			return errorcode.ErrRequestConflict
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return err
		}
		event := jsonl.Event{
			Sequence:   1,
			ID:         "event:1",
			Type:       "session.saved",
			Collection: "session",
			Key:        id,
			CreatedAt:  time.Now().UTC(),
			Payload:    payload,
		}
		if err := validateRelations(state, records, id, "session", value); err != nil {
			return err
		}
		state.remember(id, event, value)
		state.projects[id] = value.ProjectID.String()
		state.enqueue(persistenceOperation{
			kind:      persistCreateSession,
			scope:     id,
			projectID: value.ProjectID.String(),
			event:     event,
		})
		return nil
	})
}

func (r SessionRepository) List(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", "", limit)
}

func (r SessionRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, id, "", limit)
}

func (r SessionRepository) SearchTitle(ctx context.Context, title string, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", strings.ToLower(title), limit)
}

func (r SessionRepository) find(ctx context.Context, project identity.ProjectID, title string, limit int) ([]sessionmodel.Session, error) {
	values, err := list(ctx, r.storage, "session", func(value sessionmodel.Session) bool {
		return (project == "" || value.ProjectID == project) && strings.Contains(strings.ToLower(value.Title), title)
	})
	return capped(sorted(values, func(a, b sessionmodel.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), err
}

func hasEntries(records []logRecord, id string) bool {
	for _, record := range records {
		if record.scope == id && record.event.Collection == "entry" {
			return true
		}
	}
	return false
}

func (r SessionRepository) HasEntries(ctx context.Context, id identity.SessionID) (found bool, err error) {
	err = withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		if _, err := getRecord[sessionmodel.Session](records, "session", id.String()); err != nil {
			return err
		}
		found = hasEntries(records, id.String())
		return nil
	})
	return found, err
}

func (r SessionRepository) Rename(ctx context.Context, id identity.SessionID, title string, at time.Time) error {
	return r.updateName(ctx, id, func(value *sessionmodel.Session) { value.Title, value.UpdatedAt = title, at })
}

func (r SessionRepository) NameFromInput(ctx context.Context, id identity.SessionID, content string, at time.Time) error {
	return r.updateName(ctx, id, func(value *sessionmodel.Session) { value.NameFromInput(content, at) })
}

func (r SessionRepository) updateName(ctx context.Context, id identity.SessionID, update func(*sessionmodel.Session)) error {
	return withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		value, err := getRecord[sessionmodel.Session](records, "session", id.String())
		if err != nil {
			return err
		}
		update(&value)
		return appendRecord(state, records, id.String(), "session", id.String(), "session.saved", value)
	})
}

// Delete 立即移除内存中的会话及历史，文件删除排在该会话先前的写入之后；活动执行须先停止。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID, onlyEmpty bool) error {
	return withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		if _, err := getRecord[sessionmodel.Session](records, "session", id.String()); err != nil {
			if onlyEmpty && errors.Is(err, errorcode.ErrNotFound) {
				return nil
			}
			return err
		}
		if onlyEmpty && hasEntries(records, id.String()) {
			return nil
		}
		active, err := listRecords(records, "agent_execution", func(value agentexecution.ExecutionRecord) bool { return value.SessionID == id && value.Active() })
		if err != nil {
			return err
		}
		if len(active) != 0 {
			return agentmodel.ErrExecuting
		}
		state.forgetSession(id.String())
		state.enqueue(persistenceOperation{
			kind:  persistDeleteSession,
			scope: id.String(),
		})
		return nil
	})
}
