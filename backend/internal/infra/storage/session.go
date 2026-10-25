package storage

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"strings"
	"time"
)

type SessionRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 errorcode.ErrNotFound。
func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error) {
	return get[sessionmodel.Session](ctx, r.s, "session", id.String())
}

// Create 在会话创建用例中显式建立日志，首条会话事实可靠发布后才更新内存可见状态。
func (r SessionRepository) Create(ctx context.Context, v sessionmodel.Session) error {
	return r.s.view(ctx, func() error {
		if err := r.s.flushLocked(ctx); err != nil {
			return err
		}
		sessionID := v.ID.String()
		if r.s.sequences[sessionID] != 0 {
			return errorcode.ErrRequestConflict
		}
		payload, err := json.Marshal(v)
		if err != nil {
			return err
		}
		event := jsonl.Event{
			Sequence:   1,
			ID:         "event:1",
			Type:       "session.saved",
			Collection: "session",
			Key:        sessionID,
			CreatedAt:  time.Now().UTC(),
			Payload:    payload,
		}
		_, entry, err := r.s.describe(sessionID, event, v)
		if err != nil {
			return err
		}
		if err := r.s.validateEvent(sessionID, event, entry); err != nil {
			return err
		}
		if err := r.s.log.With(ctx, func(log *jsonl.Access) error {
			_, err := log.CreateSession(sessionID, v.ProjectID.String(), event)
			return err
		}); err != nil {
			return err
		}
		r.s.applyEvent(sessionID, event, entry)
		return nil
	})
}

// List 返回查询结果的独立副本，limit 为 0 时不限制。
func (r SessionRepository) List(ctx context.Context, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", "", limit)
}

// ListByProject 返回指定 Project 的对象副本，limit 为 0 时不限制。
func (r SessionRepository) ListByProject(ctx context.Context, id identity.ProjectID, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, id, "", limit)
}

// SearchTitle 按会话目录索引查询标题子串，空字符串返回完整目录。
func (r SessionRepository) SearchTitle(ctx context.Context, title string, limit int) ([]sessionmodel.Session, error) {
	return r.find(ctx, "", title, limit)
}

func (r SessionRepository) find(ctx context.Context, project identity.ProjectID, title string, limit int) ([]sessionmodel.Session, error) {
	group := collectionKey("session")
	if project != "" {
		group = lookupKey{kind: "sessions-project", owner: project.String()}
	}
	v, e := listIndexed[sessionmodel.Session](ctx, r.s, group, func(v sessionmodel.Session) bool {
		return (project == "" || v.ProjectID == project) && strings.Contains(strings.ToLower(v.Title), title)
	})
	return capped(sorted(v, func(a, b sessionmodel.Session) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}

// HasEntries 从消息分组派生会话级存在性，不解码消息正文。
func (r SessionRepository) HasEntries(ctx context.Context, id identity.SessionID) (bool, error) {
	found := false
	err := r.s.view(ctx, func() error {
		if _, err := getLocked[sessionmodel.Session](r.s, "session", id.String()); err != nil {
			return err
		}
		found = len(r.s.keys(lookupKey{kind: "entries-session", owner: id.String()})) > 0
		return nil
	})
	return found, err
}

// Rename 更新规范化的非空标题，归属和创建时间保持不变。
func (r SessionRepository) Rename(ctx context.Context, id identity.SessionID, title string, at time.Time) error {
	return r.s.view(ctx, func() error {
		v, err := getLocked[sessionmodel.Session](r.s, "session", id.String())
		if err != nil {
			return err
		}
		v.Title, v.UpdatedAt = title, at
		return r.s.appendLocked(ctx, id.String(), "session", id.String(), "session.saved", v)
	})
}

// NameFromInput 只在标题仍为空时写入，附属命名重试不能覆盖人工修改。
func (r SessionRepository) NameFromInput(ctx context.Context, id identity.SessionID, content string, at time.Time) error {
	return r.s.view(ctx, func() error {
		v, err := getLocked[sessionmodel.Session](r.s, "session", id.String())
		if err != nil {
			return err
		}
		v.NameFromInput(content, at)
		return r.s.appendLocked(ctx, id.String(), "session", id.String(), "session.saved", v)
	})
}

// Delete 在同一锁内检查活动执行、删除日志并清理内存；onlyEmpty 用于退出时的空会话清理。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID, onlyEmpty bool) error {
	return r.s.view(ctx, func() error {
		if _, err := getLocked[sessionmodel.Session](r.s, "session", id.String()); err != nil {
			if onlyEmpty && errors.Is(err, errorcode.ErrNotFound) {
				return nil
			}
			return err
		}
		if onlyEmpty && len(r.s.keys(lookupKey{kind: "entries-session", owner: id.String()})) > 0 {
			return nil
		}
		if len(r.s.keys(lookupKey{kind: "active-session", owner: id.String()})) > 0 {
			return agentmodel.ErrExecuting
		}
		if err := r.s.flushLocked(ctx); err != nil {
			return err
		}
		if err := r.s.log.With(ctx, func(log *jsonl.Access) error {
			return log.DeleteSession(id.String())
		}); err != nil {
			return err
		}
		// 日志删除成功后才释放历史和索引，文件操作失败时保留会话状态。
		for key := range r.s.state.groups[lookupKey{kind: "entries-session", owner: id.String()}] {
			delete(r.s.histories, r.s.state.entries[key].record.AgentID)
		}
		r.s.state.removeScope(id.String())
		delete(r.s.sequences, id.String())
		return nil
	})
}
