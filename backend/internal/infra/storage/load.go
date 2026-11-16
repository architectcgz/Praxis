package storage

import (
	"context"
	"praxis/internal/clock"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func (r SessionRepository) Load(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	info, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	store, err := r.OpenLog(ctx, id)
	if err != nil {
		return nil, err
	}
	log := &entryLog{sessionID: id, store: store, dirty: make(map[logKey]struct{})}
	if err := log.initialize(ctx); err != nil {
		return nil, err
	}
	locations, err := store.Runs(ctx)
	if err != nil {
		return nil, err
	}
	heads, err := store.AgentHeads(ctx)
	if err != nil {
		return nil, err
	}
	data := sessionmodel.Open(id)
	for _, location := range locations {
		data.SetRun(location.Record)
		if location.EndEntryID == "" {
			state, err := log.readRun(ctx, location)
			if err != nil {
				return nil, err
			}
			data.RunStates[state.ID] = state
		}
		if location.EndEntryID != "" {
			data.RunEndEntries[location.Record.ID] = location.EndEntryID
		}
	}
	for _, head := range heads {
		if head.AgentID == info.MainAgentID() {
			data.HeadEntryID = head.EntryID
		} else {
			data.AgentHistories[head.AgentID] = sessionmodel.AgentHistory{HeadEntryID: head.EntryID}
		}
	}
	owner := sessionmodel.New(data, log, clock.UTCClock{}, identity.SecureIDGenerator{})
	if err := recoverRuns(ctx, owner, log, locations, info.State, clock.UTCClock{}); err != nil {
		return nil, err
	}
	return owner, nil
}
