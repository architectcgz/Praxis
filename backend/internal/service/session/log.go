package session

import (
	"context"
	"fmt"
	agentrun "praxis/internal/core/agent/run"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/repository"
)

// LogStore 绑定会话的物理定位与派生目录；底层不解释业务日志载荷。
type LogStore interface {
	ReadFrom(context.Context, identity.BranchID, identity.AgentID, int64, bool, func(jsonl.Position, []byte) error) error
	ReadAt(context.Context, identity.BranchID, identity.AgentID, jsonl.Position) ([]byte, error)
	Append(context.Context, identity.BranchID, identity.AgentID, []byte) (jsonl.Position, error)
	CreateSubagent(context.Context, identity.BranchID, identity.AgentID) error
	Subagents(context.Context, identity.BranchID) ([]identity.AgentID, error)
	Progress(context.Context, identity.BranchID, identity.AgentID) (int64, error)
	Index(context.Context, repository.HistoryUpdate) error
	Node(context.Context, string) (sessionentry.Node, error)
	PathNodes(context.Context, string) ([]sessionentry.Node, error)
	TreeNodes(context.Context) ([]sessionentry.Node, error)
	EntryLocations(context.Context, []string) ([]repository.IndexedEntry, error)
	Runs(context.Context) ([]repository.RunLocation, error)
	Run(context.Context, identity.RunID) (repository.RunLocation, error)
	AgentHeads(context.Context) ([]repository.AgentHead, error)
	RecoveryRuns(context.Context) ([]identity.RunID, error)
	RunEntries(context.Context, identity.RunID) ([]string, error)
	Requests(context.Context, []identity.TurnID) ([]agentrun.ModelRequestRecord, error)
	RemoveBranch(context.Context, identity.BranchID) error
}

type logKey struct {
	branch identity.BranchID
	agent  identity.AgentID
}

// eventLog 的索引推进与追加均由 Session 的同步提交边界串行化。
// dirty 只登记需要补齐的日志，不持有已访问历史正文。
type eventLog struct {
	sessionID identity.SessionID
	store     LogStore
	dirty     map[logKey]struct{}
}

var _ sessionmodel.EventLog = (*eventLog)(nil)

func (l *eventLog) initialize(ctx context.Context, branches []sessionmodel.Branch) error {
	for _, branch := range branches {
		agents, err := l.store.Subagents(ctx, branch.ID)
		if err != nil {
			return err
		}
		agents = append(agents, identity.AgentID(l.sessionID))
		for _, agent := range agents {
			if err := l.indexTail(ctx, logKey{branch.ID, agent}, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *eventLog) indexTail(ctx context.Context, key logKey, recoverTail bool) error {
	progress, err := l.store.Progress(ctx, key.branch, key.agent)
	if err != nil {
		return err
	}
	return l.store.ReadFrom(ctx, key.branch, key.agent, progress, recoverTail, func(position jsonl.Position, line []byte) error {
		event, err := decodeEvent(line)
		if err != nil {
			return err
		}
		if err := validateLoadedEvent(event); err != nil {
			return err
		}
		if event.BranchID != key.branch || event.AgentID != key.agent {
			return fmt.Errorf("log ownership mismatch for session %s branch %s agent %s", l.sessionID, key.branch, key.agent)
		}
		return l.indexEvent(ctx, event, position)
	})
}

func (l *eventLog) Sync(ctx context.Context) error {
	for key := range l.dirty {
		if err := l.indexTail(ctx, key, false); err != nil {
			return fmt.Errorf("complete history index: %w", err)
		}
		delete(l.dirty, key)
	}
	return nil
}

// AppendEvent 单独报告完整日志是否已写入；索引失败不要求 Session 永久保留已落盘正文。
func (l *eventLog) AppendEvent(ctx context.Context, event sessionmodel.Event) (bool, error) {
	if err := l.Sync(ctx); err != nil {
		return false, err
	}
	if event.AgentID == "" {
		event.AgentID = identity.AgentID(l.sessionID)
	}
	data, err := encodeEvent(event)
	if err != nil {
		return false, err
	}
	key := logKey{event.BranchID, event.AgentID}
	position, err := l.store.Append(ctx, key.branch, key.agent, data)
	if err != nil {
		l.dirty[key] = struct{}{}
		return false, err
	}
	if err := l.indexEvent(ctx, event, position); err != nil {
		l.dirty[key] = struct{}{}
		return true, fmt.Errorf("index appended history: %w", err)
	}
	return true, nil
}

func (l *eventLog) indexEvent(ctx context.Context, event sessionmodel.Event, position jsonl.Position) error {
	update := repository.HistoryUpdate{Position: repository.LogPosition{
		BranchID: event.BranchID,
		AgentID:  event.AgentID,
		Offset:   position.Offset,
		Length:   position.Length,
	}}
	var entry *sessionentry.ContextEntry
	switch value := event.Payload.(type) {
	case agentrun.RunRecord:
		update.Run = new(value.HistorySummary())
	case sessionmodel.RunEnded:
		update.Run, entry = new(value.Run.HistorySummary()), &value.Entry
		update.EndEntryID = value.Entry.ID
	case sessionmodel.ModelRequestSaved:
		update.Run, update.Request = new(value.Run.HistorySummary()), &value.Request
	case sessionentry.ContextEntry:
		entry = &value
	}
	if entry != nil {
		update.Entry = new(sessionentry.NewNode(event.BranchID, event.AgentID, *entry))
		for _, block := range entry.Content {
			switch block := block.(type) {
			case sessionentry.ToolCallBlock:
				update.Calls = append(update.Calls, block.CallID)
			case sessionentry.ToolResultBlock:
				update.Results = append(update.Results, block.CallID)
			}
		}
	}
	return l.store.Index(ctx, update)
}

func (l *eventLog) IndexedNodes(ctx context.Context, ids []string) ([]sessionentry.Node, error) {
	locations, err := l.store.EntryLocations(ctx, ids)
	if err != nil {
		return nil, err
	}
	nodes := make([]sessionentry.Node, len(locations))
	for index, location := range locations {
		nodes[index] = location.Node
	}
	return nodes, nil
}

func (l *eventLog) CreateSubagent(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID) error {
	return l.store.CreateSubagent(ctx, branchID, agentID)
}
func (l *eventLog) Node(ctx context.Context, id string) (sessionentry.Node, error) {
	return l.store.Node(ctx, id)
}
func (l *eventLog) PathNodes(ctx context.Context, head string) ([]sessionentry.Node, error) {
	return l.store.PathNodes(ctx, head)
}
func (l *eventLog) TreeNodes(ctx context.Context) ([]sessionentry.Node, error) {
	return l.store.TreeNodes(ctx)
}
func (l *eventLog) Requests(ctx context.Context, ids []identity.TurnID) ([]agentrun.ModelRequestRecord, error) {
	return l.store.Requests(ctx, ids)
}

func (l *eventLog) Entries(ctx context.Context, ids []string) ([]*sessionentry.ContextEntry, error) {
	locations, err := l.store.EntryLocations(ctx, ids)
	if err != nil {
		return nil, err
	}
	entries := make([]*sessionentry.ContextEntry, 0, len(locations))
	for _, location := range locations {
		event, err := l.readEvent(ctx, location.Position)
		if err != nil {
			return nil, fmt.Errorf("read entry %s: %w", location.Node.ID, err)
		}
		var entry sessionentry.ContextEntry
		switch value := event.Payload.(type) {
		case sessionentry.ContextEntry:
			entry = value
		case sessionmodel.RunEnded:
			entry = value.Entry
		default:
			return nil, fmt.Errorf("entry %s does not locate an entry record", location.Node.ID)
		}
		if entry.ID != location.Node.ID || event.BranchID != location.Node.BranchID || event.AgentID != location.Node.AgentID {
			return nil, fmt.Errorf("entry %s location does not match its indexed identity", location.Node.ID)
		}
		entries = append(entries, &entry)
	}
	return entries, nil
}

func (l *eventLog) readEvent(ctx context.Context, position repository.LogPosition) (sessionmodel.Event, error) {
	line, err := l.store.ReadAt(ctx, position.BranchID, position.AgentID, jsonl.Position{Offset: position.Offset, Length: position.Length})
	if err != nil {
		return sessionmodel.Event{}, err
	}
	event, err := decodeEvent(line)
	if err != nil {
		return event, err
	}
	return event, validateLoadedEvent(event)
}

func (l *eventLog) Run(ctx context.Context, id identity.RunID) (agentrun.RunRecord, error) {
	location, err := l.store.Run(ctx, id)
	if err != nil {
		return agentrun.RunRecord{}, err
	}
	return l.readRun(ctx, location)
}

func (l *eventLog) readRun(ctx context.Context, location repository.RunLocation) (agentrun.RunRecord, error) {
	event, err := l.readEvent(ctx, location.Position)
	if err != nil {
		return agentrun.RunRecord{}, err
	}
	var record agentrun.RunRecord
	switch value := event.Payload.(type) {
	case agentrun.RunRecord:
		record = value
	case sessionmodel.RunEnded:
		record = value.Run
	case sessionmodel.ModelRequestSaved:
		record = value.Run
	default:
		return record, errorcode.ErrNotFound
	}
	if record.ID != location.Record.ID {
		return record, fmt.Errorf("run %s has an invalid history position", location.Record.ID)
	}
	return record, nil
}
