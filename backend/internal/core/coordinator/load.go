package coordinator

import (
	"context"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Coordinator) GetSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value := s.registry.get(id)
	if value == nil {
		return nil, errorcode.ErrNotFound
	}
	return value, nil
}
func (s *Coordinator) LoadSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.loadSession(ctx, id)
}
func (s *Coordinator) loadSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	if value := s.registry.get(id); value != nil {
		return value, nil
	}
	owner, err := s.sessionRecords.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	config, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	config.ApprovalMode = security.EffectiveApprovalMode(config.GlobalApprovalMode, info.ApprovalMode)
	owner.Configure(config, info.ModelChoice)
	s.registry.add(id, owner)
	s.mu.Lock()
	s.schedules[id] = &sessionSchedule{sessionID: id, session: owner}
	s.mu.Unlock()
	return owner, nil
}
func (s *Coordinator) releaseSession(ctx context.Context, id identity.SessionID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.registry.remove(id)
	return nil
}
