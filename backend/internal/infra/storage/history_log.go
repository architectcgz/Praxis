package storage

import (
	"context"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/repository"
)

type logKey struct{ agent identity.AgentID }
type entryLog struct {
	sessionID identity.SessionID
	store     *SessionLog
	dirty     map[logKey]struct{}
}

var _ sessionmodel.EntryLog = (*entryLog)(nil)

func (l *entryLog) initialize(ctx context.Context) error {
	agents, err := l.store.Subagents(ctx)
	if err != nil {
		return err
	}
	agents = append(agents, identity.AgentID(l.sessionID))
	for _, agent := range agents {
		if err := l.indexTail(ctx, logKey{agent}, true); err != nil {
			return err
		}
	}
	return nil
}
func (l *entryLog) indexTail(ctx context.Context, key logKey, recoverTail bool) error {
	progress, err := l.store.Progress(ctx, key.agent)
	if err != nil {
		return err
	}
	return l.store.ReadFrom(ctx, key.agent, progress, recoverTail, func(position jsonl.Position, line []byte) error {
		stored, err := decodeEntry(line)
		if err != nil {
			return err
		}
		if stored.AgentID != key.agent {
			return fmt.Errorf("history ownership mismatch for agent %s", key.agent)
		}
		return l.indexEntry(ctx, key.agent, stored.Entry, position)
	})
}
func (l *entryLog) Sync(ctx context.Context) error {
	for key := range l.dirty {
		if err := l.indexTail(ctx, key, false); err != nil {
			return err
		}
		delete(l.dirty, key)
	}
	return nil
}
func (l *entryLog) AppendEntry(ctx context.Context, agent identity.AgentID, entry sessionentry.ContextEntry) (bool, error) {
	if err := l.Sync(ctx); err != nil {
		return false, err
	}
	data, err := encodeEntry(agent, entry)
	if err != nil {
		return false, err
	}
	pos, err := l.store.Append(ctx, agent, data)
	if err != nil {
		l.dirty[logKey{agent}] = struct{}{}
		return false, err
	}
	if err := l.indexEntry(ctx, agent, entry, pos); err != nil {
		l.dirty[logKey{agent}] = struct{}{}
		return true, err
	}
	return true, nil
}
func (l *entryLog) indexEntry(ctx context.Context, agent identity.AgentID, e sessionentry.ContextEntry, pos jsonl.Position) error {
	update := repository.HistoryUpdate{
		Position: repository.LogPosition{AgentID: agent, Offset: pos.Offset, Length: pos.Length},
		Entry:    new(sessionentry.NewNode(agent, e)),
	}
	id := identity.RunID(e.RunID)
	prior := l.store.index.runs[id]
	summary := sessionentry.ApplySummary(prior.Record, &e)
	if summary.ID != "" {
		update.Run = &summary
	}
	if e.IsRun() {
		update.EndEntryID = e.ID
	}
	update.Request, _ = e.RequestFact()
	for _, block := range e.Content {
		switch b := block.(type) {
		case sessionentry.ToolCallBlock:
			update.Calls = append(update.Calls, b.CallID)
		case sessionentry.ToolResultBlock:
			update.Results = append(update.Results, b.CallID)
		}
	}
	return l.store.Index(ctx, update)
}
func (l *entryLog) IndexedNodes(ctx context.Context, ids []string) ([]sessionentry.Node, error) {
	locations, err := l.store.EntryLocations(ctx, ids)
	if err != nil {
		return nil, err
	}
	nodes := make([]sessionentry.Node, len(locations))
	for i, v := range locations {
		nodes[i] = v.Node
	}
	return nodes, nil
}
func (l *entryLog) CreateSubagent(ctx context.Context, id identity.AgentID) error {
	return l.store.CreateSubagent(ctx, id)
}
func (l *entryLog) Node(ctx context.Context, id string) (sessionentry.Node, error) {
	return l.store.Node(ctx, id)
}
func (l *entryLog) PathNodes(ctx context.Context, id string) ([]sessionentry.Node, error) {
	return l.store.PathNodes(ctx, id)
}
func (l *entryLog) Requests(ctx context.Context, ids []identity.TurnID) ([]sessionentry.ModelRequestRecord, error) {
	return l.store.Requests(ctx, ids)
}
func (l *entryLog) Entries(ctx context.Context, ids []string) ([]*sessionentry.ContextEntry, error) {
	locations, err := l.store.EntryLocations(ctx, ids)
	if err != nil {
		return nil, err
	}
	values := make([]*sessionentry.ContextEntry, 0, len(locations))
	for _, v := range locations {
		line, err := l.store.ReadAt(ctx, v.Position.AgentID, jsonl.Position{Offset: v.Position.Offset, Length: v.Position.Length})
		if err != nil {
			return nil, err
		}
		stored, err := decodeEntry(line)
		if err != nil {
			return nil, err
		}
		if stored.Entry.ID != v.Node.ID || stored.AgentID != v.Node.AgentID {
			return nil, fmt.Errorf("history position does not match entry %s", v.Node.ID)
		}
		values = append(values, &stored.Entry)
	}
	return values, nil
}
func (l *entryLog) Run(ctx context.Context, id identity.RunID) (sessionentry.RunRecord, error) {
	location, err := l.store.Run(ctx, id)
	if err != nil {
		return sessionentry.RunRecord{}, err
	}
	return l.readRun(ctx, location)
}
func (l *entryLog) readRun(ctx context.Context, location repository.RunLocation) (sessionentry.RunRecord, error) {
	ids, err := l.store.RunEntries(ctx, location.Record.ID)
	if err != nil {
		return sessionentry.RunRecord{}, err
	}
	entries, err := l.Entries(ctx, ids)
	if err != nil {
		return sessionentry.RunRecord{}, err
	}
	var record sessionentry.RunRecord
	for _, e := range entries {
		record = sessionentry.Apply(record, e)
	}
	return record, nil
}
