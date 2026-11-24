package storage

import (
	"context"
	"fmt"
	"praxis/internal/engine/session"
	"praxis/internal/entry"
	"praxis/internal/identity"
	"slices"
	"time"
)

const recoveryBatchSize = 200

func (r SessionRepository) Load(ctx context.Context, id identity.SessionID) (*session.Session, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	store, err := r.OpenLog(ctx, id)
	if err != nil {
		return nil, err
	}
	log := &entryLog{store: store}
	data := session.Open(id)
	agents := store.Agents()
	slices.Sort(agents)
	for _, agentID := range agents {
		agentCtx := data.AgentContexts[agentID]
		if agentCtx == nil {
			agentCtx = &session.AgentContext{}
			data.AgentContexts[agentID] = agentCtx
		}
		if err := recoverAgent(ctx, log, agentID, agentCtx); err != nil {
			_ = log.Close(context.Background())
			return nil, err
		}
	}
	return session.New(data, log), nil
}

func recoverAgent(ctx context.Context, log *entryLog, agentID identity.AgentID, agentCtx *session.AgentContext) error {
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
			now := time.Now().UTC()
			for _, receipt := range entry.MissingToolResultEntries(runEntries, now) {
				if err := log.Append(ctx, agentID, &receipt); err != nil {
					return err
				}
			}
			terminal := entry.Entry{
				ID:        identity.NewEntryID(),
				RunID:     last.RunID,
				CreatedAt: now,
				Payload: entry.RunInterrupted{
					RunStats: entry.RunStats{StartedAt: last.CreatedAt, EndedAt: now},
					Reason:   string(entry.RunFailureInterrupted),
				},
			}
			if err := log.CommitTerminal(ctx, agentID, &terminal); err != nil {
				return err
			}
		}
	}
	return recoverAgentContext(ctx, log, agentID, agentCtx)
}

func readRunEntries(ctx context.Context, log *entryLog, agentID identity.AgentID, runID identity.RunID) ([]*entry.Entry, error) {
	var cursor *int64
	entries := make([]*entry.Entry, 0)
	for {
		page, err := log.ReadBackward(ctx, agentID, cursor, recoveryBatchSize)
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

func recoverAgentContext(ctx context.Context, log *entryLog, agentID identity.AgentID, agentCtx *session.AgentContext) error {
	var cursor *int64
	entries := make([]*entry.Entry, 0)
	for {
		page, err := log.ReadBackward(ctx, agentID, cursor, recoveryBatchSize)
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
	agentCtx.Entries = entries
	return nil
}
