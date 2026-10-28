package storage

import (
	"cmp"
	"context"
	"errors"
	"os"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"strings"
	"time"
)

type SessionRepository struct{ storage *Store }

func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return sessionmodel.Session{}, err
	}
	defer r.storage.mu.RUnlock()
	value, ok := r.storage.state.sessions[id]
	if !ok {
		return sessionmodel.Session{}, errorcode.ErrNotFound
	}
	return value, nil
}

// Create 先创建会话日志并写入首条事实，成功后发布会话内存状态。
func (r SessionRepository) Create(ctx context.Context, value sessionmodel.Session) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	if _, exists := r.storage.state.sessions[value.ID]; exists {
		return errorcode.ErrRequestConflict
	}
	event, err := newEvent(1, "session.saved", "session", value.ID.String(), value)
	if err != nil {
		return err
	}
	if _, err := r.storage.log.CreateSession(ctx, value.ID.String(), value.ProjectID.String(), event); err != nil {
		return err
	}
	r.storage.state.sessions[value.ID] = value
	r.storage.state.sequences[value.ID.String()] = event.Sequence
	return nil
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
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	values := make([]sessionmodel.Session, 0)
	for _, value := range r.storage.state.sessions {
		if (project == "" || value.ProjectID == project) && strings.Contains(strings.ToLower(value.Title), title) {
			values = append(values, value)
		}
	}
	return capped(sorted(values, func(a, b sessionmodel.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), nil
}

func (r SessionRepository) HasEntries(ctx context.Context, id identity.SessionID) (bool, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return false, err
	}
	defer r.storage.mu.RUnlock()
	if _, ok := r.storage.state.sessions[id]; !ok {
		return false, errorcode.ErrNotFound
	}
	return len(r.storage.state.entryOrder[id]) != 0, nil
}
func (r SessionRepository) Rename(ctx context.Context, id identity.SessionID, title string, at time.Time) error {
	return r.updateName(ctx, id, func(value *sessionmodel.Session) { value.Title, value.UpdatedAt = title, at })
}
func (r SessionRepository) NameFromInput(ctx context.Context, id identity.SessionID, content string, at time.Time) error {
	return r.updateName(ctx, id, func(value *sessionmodel.Session) { value.NameFromInput(content, at) })
}
func (r SessionRepository) updateName(ctx context.Context, id identity.SessionID, update func(*sessionmodel.Session)) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	value, ok := r.storage.state.sessions[id]
	if !ok {
		return errorcode.ErrNotFound
	}
	prior := value
	update(&value)
	if value == prior {
		return nil
	}
	if err := r.storage.appendEventLocked(ctx, id, "session.saved", "session", id.String(), value); err != nil {
		return err
	}
	r.storage.state.sessions[id] = value
	return nil
}

// Delete 先删除日志，再移除会话及其所有内存数据；业务层负责确认活动执行已经停止。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID, onlyEmpty bool) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	if _, ok := r.storage.state.sessions[id]; !ok {
		if onlyEmpty {
			return nil
		}
		return errorcode.ErrNotFound
	}
	if onlyEmpty && len(r.storage.state.entryOrder[id]) != 0 {
		return nil
	}
	if err := r.storage.log.DeleteSession(ctx, id.String()); err != nil {
		return err
	}
	r.storage.state.removeSession(id)
	return nil
}

// SessionLogPath 是显式文件访问，普通仓储读取不访问日志。
func (r SessionRepository) SessionLogPath(ctx context.Context, id identity.SessionID) (string, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return "", err
	}
	defer r.storage.mu.RUnlock()
	if _, ok := r.storage.state.sessions[id]; !ok {
		return "", errorcode.ErrNotFound
	}
	path, err := r.storage.log.ExistingPath(ctx, id.String())
	if errors.Is(err, os.ErrNotExist) {
		return "", errorcode.ErrNotFound
	}
	return path, err
}
