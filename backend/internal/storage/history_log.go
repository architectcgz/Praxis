package storage

import (
	"context"
	"fmt"
	"praxis/internal/engine/session"
	"praxis/internal/entry"
	"praxis/internal/identity"
)

type entryLog struct {
	store *SessionLog
}

var _ session.HistoryStore = (*entryLog)(nil)

func (l *entryLog) Append(ctx context.Context, agentID identity.AgentID, value *entry.Entry) error {
	data, err := encodeEntry(agentID, *value)
	if err != nil {
		return err
	}
	writer, err := l.store.writer(agentID)
	if err != nil {
		return err
	}
	position, err := writer.Append(ctx, data)
	if err == nil {
		value.HistoryEnd = position.Offset + position.Length
	}
	return err
}

func (l *entryLog) CommitTerminal(ctx context.Context, agentID identity.AgentID, value *entry.Entry) error {
	data, err := encodeEntry(agentID, *value)
	if err != nil {
		return err
	}
	writer, err := l.store.writer(agentID)
	if err != nil {
		return err
	}
	position, err := writer.CommitTerminal(ctx, data)
	if err == nil {
		value.HistoryEnd = position.Offset + position.Length
	}
	return err
}

func (l *entryLog) ReadBackward(ctx context.Context, agentID identity.AgentID, cursor *int64, limit int) (session.HistoryPage, error) {
	records, nextCursor, hasMore, err := l.store.readBackward(ctx, agentID, cursor, limit)
	if err != nil {
		return session.HistoryPage{}, err
	}
	entries := make([]entry.Entry, 0, len(records))
	for _, record := range records {
		stored, err := decodeEntry(record.Data)
		if err != nil {
			return session.HistoryPage{}, fmt.Errorf("decode agent %s history at offset %d: %w", agentID, record.Offset, err)
		}
		if stored.AgentID != agentID {
			return session.HistoryPage{}, fmt.Errorf("history ownership mismatch for agent %s", agentID)
		}
		stored.Entry.HistoryEnd = record.Offset + record.Length
		entries = append(entries, stored.Entry)
	}
	return session.HistoryPage{
		Entries:    entries,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (l *entryLog) CreateSubagent(ctx context.Context, agentID identity.AgentID) error {
	return l.store.CreateSubagent(ctx, agentID)
}

func (l *entryLog) RemoveSubagent(ctx context.Context, agentID identity.AgentID) error {
	return l.store.RemoveSubagent(ctx, agentID)
}

func (l *entryLog) Close(ctx context.Context) error {
	return l.store.Close(ctx)
}
