package storage

import (
	"cmp"
	"context"
	"errors"
	timingmodel "praxis/internal/core/timing"
	"praxis/internal/fault"
)

// TimingStore 将计时更新放入所属 Session 日志，不影响主业务事务。
type TimingStore struct{ Store *Store }

// Save 拒绝旧版本及终态回退，重复保存相同版本时要求内容一致。
func (r TimingStore) Save(ctx context.Context, v timingmodel.Record) error {
	return r.Store.InTx(ctx, func(ctx context.Context) error {
		old, e := get[timingmodel.Record](ctx, r.Store, "timing", v.ID)
		if e == nil {
			if v.Revision < old.Revision {
				return errors.New("stale timing version rejected")
			}
			if v.Revision == old.Revision {
				if same(old, v) {
					return nil
				}
				return fault.ErrRequestConflict
			}
			if old.Status != timingmodel.Running || old.Operation != v.Operation || old.ParentID != v.ParentID || !old.StartedAt.Equal(v.StartedAt) {
				return fault.ErrRequestConflict
			}
		} else if !errors.Is(e, fault.ErrNotFound) {
			return e
		}
		return save(ctx, r.Store, v.SessionID, "timing", v.ID, "timing.saved", v)
	})
}

// ListAgent 返回指定 Agent 按开始时间倒序的计时记录。
func (r TimingStore) ListAgent(ctx context.Context, id string, limit int) ([]timingmodel.Record, error) {
	v, e := listIndexed[timingmodel.Record](ctx, r.Store, lookupKey{kind: "timings-agent", owner: id}, nil)
	return capped(sorted(v, func(a, b timingmodel.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	}), limit), e
}

// RecoverInterrupted 将未结束计时收敛为未知时长，不捏造完成时间。
func (r TimingStore) RecoverInterrupted(ctx context.Context) error {
	values, e := listIndexed[timingmodel.Record](ctx, r.Store, lookupKey{kind: "running-timings"}, nil)
	if e != nil {
		return e
	}
	for _, v := range values {
		v.Status = timingmodel.Interrupted
		v.Revision++
		if e = r.Save(ctx, v); e != nil {
			return e
		}
	}
	return nil
}
