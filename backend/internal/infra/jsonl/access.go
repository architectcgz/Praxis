package jsonl

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Access 只在 With 回调内有效；其方法不重复获取文件锁，不得逃逸到后台任务。
type Access struct {
	log *Log
	ctx context.Context
}

// With 将读取、条件检查和单事件提交置于同一文件锁内，覆盖跨 Repository 的竞争。
// 回调不能调用外部业务，也不能再次调用同一 Log 的 With 或 Close。
func (s *Log) With(ctx context.Context, action func(*Access) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Check(); err != nil {
		return err
	}
	return action(&Access{log: s, ctx: ctx})
}

func (a *Access) Scopes() ([]Scope, error)                      { return a.log.Scopes(a.ctx) }
func (a *Access) Sequence(sessionID string) uint64              { return a.log.Sequence(sessionID) }
func (a *Access) Project(sessionID string) string               { return a.log.Project(sessionID) }
func (a *Access) ExistingPath(sessionID string) (string, error) { return a.log.ExistingPath(sessionID) }

// ReadSession 只生成调用方需要的临时结果，不在 Log 中缓存业务正文。
func (a *Access) ReadSession(sessionID string, visit func(Event) error) error {
	return a.log.read(a.ctx, Scope{ID: sessionID, ProjectID: a.log.Project(sessionID)}, visit)
}

func (a *Access) ReadProject(visit func(Event) error) error {
	return a.log.read(a.ctx, Scope{}, visit)
}

// CreateSession 由会话创建用例显式调用，首条记录和文件一起可靠发布，不覆盖已有日志。
func (a *Access) CreateSession(sessionID, projectID string, first Event) (Event, error) {
	first, err := nextEvent(0, first)
	if err != nil {
		return Event{}, err
	}
	if err := a.log.createSession(a.ctx, sessionID, projectID, first); err != nil {
		return Event{}, err
	}
	return first, nil
}

// DeleteSession 删除会话日志并释放游标，调用方负责协调活动执行和业务状态。
func (a *Access) DeleteSession(sessionID string) error {
	return a.log.deleteSession(a.ctx, sessionID)
}

// AppendSession 只追加已有会话日志，文件丢失时返回错误，不负责创建或补建。
func (a *Access) AppendSession(sessionID, projectID string, event Event) (Event, error) {
	event, err := nextEvent(a.log.Sequence(sessionID), event)
	if err != nil {
		return Event{}, err
	}
	if err := a.log.appendEvent(a.ctx, sessionID, projectID, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// AppendProject 追加在资源初始化阶段已经建立的项目汇总日志。
func (a *Access) AppendProject(event Event) (Event, error) {
	event, err := nextEvent(a.log.Sequence(""), event)
	if err != nil {
		return Event{}, err
	}
	if err := a.log.appendProjectEvent(a.ctx, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func nextEvent(sequence uint64, event Event) (Event, error) {
	if sequence == ^uint64(0) {
		return Event{}, errors.New("log sequence exhausted")
	}
	event.Sequence = sequence + 1
	event.ID = fmt.Sprintf("event:%d", event.Sequence)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event, nil
}
