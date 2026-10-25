package jsonl

import (
	"context"
	"errors"
	"fmt"
)

// Access 只在 With 回调内有效；其方法不重复获取文件锁，不得逃逸到后台任务。
type Access struct {
	log *Log
	ctx context.Context
}

// With 串行化后台文件操作和显式日志路径查询，不承担业务状态一致性。
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

func (a *Access) ExistingPath(sessionID string) (string, error) { return a.log.ExistingPath(sessionID) }

// CreateSession 由顺序写入者调用，不覆盖已有日志，也不保证断电后已保存。
func (a *Access) CreateSession(sessionID, projectID string, first Event) (Event, error) {
	if err := validateNextEvent(0, first); err != nil {
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
	if err := validateNextEvent(a.log.sequences[sessionID], event); err != nil {
		return Event{}, err
	}
	if err := a.log.appendEvent(a.ctx, sessionID, projectID, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// AppendProject 追加在资源初始化阶段已经建立的项目汇总日志。
func (a *Access) AppendProject(event Event) (Event, error) {
	if err := validateNextEvent(a.log.sequences[""], event); err != nil {
		return Event{}, err
	}
	if err := a.log.appendProjectEvent(a.ctx, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// validateNextEvent 不重新分配内存状态确定的序号；不连续时停止写入，而不是掩盖日志缺口。
func validateNextEvent(sequence uint64, event Event) error {
	if sequence == ^uint64(0) {
		return errors.New("log sequence exhausted")
	}
	if event.Sequence != sequence+1 || event.ID != fmt.Sprintf("event:%d", event.Sequence) || event.CreatedAt.IsZero() {
		return errors.New("invalid queued event identity or sequence")
	}
	return nil
}
