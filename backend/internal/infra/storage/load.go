package storage

import (
	"context"
	"fmt"
	"praxis/internal/clock"
	sessionentry "praxis/internal/core/entry"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"slices"
)

func (r SessionRepository) Load(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	store, err := r.OpenLog(ctx, id)
	if err != nil {
		return nil, err
	}
	log := &entryLog{store: store}
	data := sessionmodel.Open(id)
	owner := sessionmodel.New(data, log, clock.UTCClock{}, identity.SecureIDGenerator{})
	agents := store.Agents()
	slices.Sort(agents)
	for _, agentID := range agents {
		if agentID != identity.AgentID(id) {
			owner.RegisterAgent(agentID)
		}
		if err := recoverAgent(ctx, owner, log, agentID); err != nil {
			_ = owner.Close(context.Background())
			return nil, err
		}
	}
	return owner, nil
}

func recoverAgent(ctx context.Context, owner *sessionmodel.Session, log *entryLog, agentID identity.AgentID) error {
	page, err := log.ReadBackward(ctx, agentID, nil, 1)
	if err != nil {
		return err
	}
	if len(page.Entries) > 0 {
		last := page.Entries[len(page.Entries)-1]
		if !last.IsRunTerminal() {
			if last.RunID == "" {
				return fmt.Errorf("history entry %s has no RunID for recovery", last.ID)
			}
			runEntries, err := readRunEntries(ctx, log, agentID, last.RunID)
			if err != nil {
				return err
			}
			now := clock.UTCClock{}.Now()
			for _, receipt := range sessionentry.MissingToolResultEntries(runEntries, now) {
				if err := log.Append(ctx, agentID, &receipt); err != nil {
					return err
				}
			}
			terminal := sessionentry.Entry{
				ID:        "run:" + last.RunID,
				RunID:     last.RunID,
				CreatedAt: now,
				Payload: sessionentry.RunInterrupted{
					RunStats: sessionentry.RunStats{StartedAt: last.CreatedAt, EndedAt: now},
					Reason:   string(sessionentry.RunFailureInterrupted),
				},
			}
			if _, err := owner.CommitTerminal(ctx, agentID, terminal); err != nil {
				return err
			}
		}
	}
	return recoverAgentContext(ctx, owner, log, agentID)
}

func readRunEntries(ctx context.Context, log *entryLog, agentID identity.AgentID, runID string) ([]*sessionentry.Entry, error) {
	var cursor *int64
	entries := make([]*sessionentry.Entry, 0)
	for {
		page, err := log.ReadBackward(ctx, agentID, cursor, 200)
		if err != nil {
			return nil, err
		}
		stop := false
		for index := len(page.Entries) - 1; index >= 0; index-- {
			value := page.Entries[index]
			if value.RunID != runID {
				stop = true
				break
			}
			entries = append(entries, &value)
		}
		if stop || !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	slices.Reverse(entries)
	return entries, nil
}

func recoverAgentContext(ctx context.Context, owner *sessionmodel.Session, log *entryLog, agentID identity.AgentID) error {
	var cursor *int64
	entries := make([]*sessionentry.Entry, 0)
	for {
		page, err := log.ReadBackward(ctx, agentID, cursor, 200)
		if err != nil {
			return err
		}
		stop := false
		for index := len(page.Entries) - 1; index >= 0; index-- {
			value := page.Entries[index]
			entries = append(entries, &value)
			if value.IsSummary() {
				stop = true
				break
			}
		}
		if stop || !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	slices.Reverse(entries)
	return owner.RestoreAgentContext(agentID, entries)
}
