package session

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// RequireWritable 提前确认会话存在且处于 active 状态；仓储在实际写入锁内再次校验。
func RequireWritable(ctx context.Context, sessions repository.Session, id identity.SessionID) error {
	value, err := sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	if value.State != sessionmodel.SessionActive {
		return errorcode.ErrRequestConflict
	}
	return nil
}
