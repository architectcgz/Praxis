package storage

import (
	"context"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

type entryLog struct {
	store *SessionLog
}

var _ sessionmodel.HistoryStore = (*entryLog)(nil)

func (l *entryLog) Append(ctx context.Context, agentID identity.AgentID, value *sessionentry.Entry) error {
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

func (l *entryLog) CommitTerminal(ctx context.Context, agentID identity.AgentID, value *sessionentry.Entry) error {
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

func (l *entryLog) Sync(ctx context.Context, agentID identity.AgentID) error {
	writer, err := l.store.writer(agentID)
	if err != nil {
		return err
	}
	return writer.Sync(ctx)
}

func (l *entryLog) ReadBackward(ctx context.Context, agentID identity.AgentID, cursor *int64, limit int) (sessionmodel.HistoryPage, error) {
	records, nextCursor, hasMore, err := l.store.readBackward(ctx, agentID, cursor, limit)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	entries := make([]sessionentry.Entry, 0, len(records))
	for _, record := range records {
		stored, err := decodeEntry(record.Data)
		if err != nil {
			return sessionmodel.HistoryPage{}, fmt.Errorf("decode agent %s history at offset %d: %w", agentID, record.Offset, err)
		}
		if stored.AgentID != agentID {
			return sessionmodel.HistoryPage{}, fmt.Errorf("history ownership mismatch for agent %s", agentID)
		}
		stored.Entry.HistoryEnd = record.Offset + record.Length
		entries = append(entries, stored.Entry)
	}
	return sessionmodel.HistoryPage{
		Entries:    entries,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (l *entryLog) CreateSubagent(ctx context.Context, agentID identity.AgentID) error {
	return l.store.CreateSubagent(ctx, agentID)
}

func (l *entryLog) Close(ctx context.Context) error {
	return l.store.Close(ctx)
}
