package jsonl

import (
	"cmp"
	"context"
	"praxis/internal/identity"
	sessionmodel "praxis/internal/service/session"
	"strings"
	"time"
)

type SessionRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r SessionRepository) Get(ctx context.Context, id identity.SessionID) (sessionmodel.Session, error) {
	return get[sessionmodel.Session](ctx, r.s, "session", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r SessionRepository) Save(ctx context.Context, v sessionmodel.Session) error {
	return save(ctx, r.s, v.ID.String(), "session", v.ID.String(), "session.saved", v)
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

// HasMessages 读取消息存在性元数据，不加载消息正文或位置列表。
func (r SessionRepository) HasMessages(ctx context.Context, id identity.SessionID) (bool, error) {
	found := false
	err := r.s.view(ctx, func(readCtx context.Context) error {
		if _, err := r.Get(readCtx, id); err != nil {
			return err
		}
		found = r.s.index.hasMessages[id.String()]
		for key, change := range r.s.tx(readCtx).changes {
			if key.collection == "message" && change.entry.location.Scope == id.String() {
				found = true
				break
			}
		}
		return nil
	})
	return found, err
}

// Rename 更新规范化的非空标题，归属和创建时间保持不变。
func (r SessionRepository) Rename(ctx context.Context, id identity.SessionID, title string, at time.Time) error {
	v, e := r.Get(ctx, id)
	if e != nil {
		return e
	}
	v.Title = title
	v.UpdatedAt = at
	return r.Save(ctx, v)
}

// Delete 追加删除标记并清理所属 Session 的查询索引；业务前置条件由删除用例检查。
func (r SessionRepository) Delete(ctx context.Context, id identity.SessionID) ([]string, error) {
	if _, e := r.Get(ctx, id); e != nil {
		return nil, e
	}
	return nil, r.s.change(ctx, id.String(), "session", id.String(), "session.deleted", nil, nil)
}
