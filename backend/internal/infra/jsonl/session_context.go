package jsonl

import (
	"cmp"
	"context"
	"errors"
	"praxis/internal/fault"
	"praxis/internal/identity"
	sessionmodel "praxis/internal/service/session"
)

type ContextRepository struct{ s *Store }

// Append 幂等追加记录，身份相同但内容不一致时返回冲突。
func (r ContextRepository) Append(ctx context.Context, v sessionmodel.SessionContextEntry) error {
	existing, ok, e := r.GetByID(ctx, v.ID)
	if e != nil {
		return e
	}
	if ok {
		v.CreatedAt = existing.CreatedAt
	}
	return save(ctx, r.s, v.SessionID.String(), "context", v.ID.String(), "context.entry_appended", v)
}

// GetByID 读取指定记录，第二个返回值表示是否存在。
func (r ContextRepository) GetByID(ctx context.Context, id identity.ContextEntryID) (sessionmodel.SessionContextEntry, bool, error) {
	v, e := get[sessionmodel.SessionContextEntry](ctx, r.s, "context", id.String())
	if errors.Is(e, fault.ErrNotFound) {
		return v, false, nil
	}
	return v, e == nil, e
}

// List 返回查询结果的独立副本，limit 为 0 时不限制。
func (r ContextRepository) List(ctx context.Context, id identity.SessionID, after string, limit int) ([]sessionmodel.SessionContextEntry, error) {
	v, e := listIndexed[sessionmodel.SessionContextEntry](ctx, r.s, lookupKey{kind: "contexts-session", owner: id.String()}, nil)
	sorted(v, func(a, b sessionmodel.SessionContextEntry) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	if after != "" {
		found := false
		for i, item := range v {
			if item.ID.String() == after {
				v = v[i+1:]
				found = true
				break
			}
		}
		if !found {
			v = nil
		}
	}
	return capped(v, limit), e
}
