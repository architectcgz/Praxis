package storage

import (
	"context"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"slices"
)

// historyIndex 只保存定位、轻量投影和请求事实，正文按位置读取；加载时从 JSONL 重建。
// 已发布 Session 的读写由所属会话提交锁串行化。
type historyIndex struct {
	progress map[identity.AgentID]int64
	entries  map[string]repository.IndexedEntry
	order    map[identity.AgentID][]string
	runs     map[identity.RunID]repository.RunLocation
	requests map[identity.ModelRequestID]sessionentry.ModelRequestRecord
	calls    map[identity.RunID]map[string]bool
}

func newHistoryIndex() *historyIndex {
	return &historyIndex{
		progress: make(map[identity.AgentID]int64),
		entries:  make(map[string]repository.IndexedEntry),
		order:    make(map[identity.AgentID][]string),
		runs:     make(map[identity.RunID]repository.RunLocation),
		requests: make(map[identity.ModelRequestID]sessionentry.ModelRequestRecord),
		calls:    make(map[identity.RunID]map[string]bool),
	}
}

func (l SessionLog) Progress(ctx context.Context, agentID identity.AgentID) (int64, error) {
	return l.index.progress[agentID], ctx.Err()
}

func (l SessionLog) Index(ctx context.Context, update repository.HistoryUpdate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	position := update.Position
	if progress := l.index.progress[position.AgentID]; progress != position.Offset {
		return fmt.Errorf("history index is not contiguous: indexed %d, record offset %d", progress, position.Offset)
	}
	if update.Entry != nil {
		node := *update.Entry
		l.index.entries[node.ID] = repository.IndexedEntry{Node: node, Position: position}
		l.index.order[position.AgentID] = append(l.index.order[position.AgentID], node.ID)
		runID := identity.RunID(node.RunID)
		if l.index.calls[runID] == nil {
			l.index.calls[runID] = make(map[string]bool)
		}
		for _, id := range update.Calls {
			l.index.calls[runID][id] = false
		}
		for _, id := range update.Results {
			l.index.calls[runID][id] = true
		}
	}
	if update.Run != nil {
		prior := l.index.runs[update.Run.ID]
		if prior.Record.ID != "" {
			position = prior.Position
		}
		end := update.EndEntryID
		if end == "" {
			end = prior.EndEntryID
		}
		l.index.runs[update.Run.ID] = repository.RunLocation{Record: *update.Run, EndEntryID: end, Position: position}
	}
	if update.Request != nil {
		request := *update.Request
		request.Partial = nil
		l.index.requests[request.ID] = request
	}
	l.index.progress[update.Position.AgentID] = update.Position.Offset + update.Position.Length
	return nil
}

func (l SessionLog) Node(ctx context.Context, id string) (sessionentry.Node, error) {
	if err := ctx.Err(); err != nil {
		return sessionentry.Node{}, err
	}
	value, ok := l.index.entries[id]
	if !ok {
		return sessionentry.Node{}, errorcode.ErrNotFound
	}
	return value.Node, nil
}

func (l SessionLog) PathNodes(ctx context.Context, head string) ([]sessionentry.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if head == "" {
		return nil, nil
	}
	value, ok := l.index.entries[head]
	if !ok {
		return nil, errorcode.ErrNotFound
	}
	nodes := make([]sessionentry.Node, 0)
	for _, id := range l.index.order[value.Node.AgentID] {
		nodes = append(nodes, l.index.entries[id].Node)
		if id == head {
			break
		}
	}
	return nodes, nil
}

func (l SessionLog) EntryLocations(ctx context.Context, ids []string) ([]repository.IndexedEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([]repository.IndexedEntry, 0, len(ids))
	for _, id := range ids {
		if value, ok := l.index.entries[id]; ok {
			values = append(values, value)
		}
	}
	return values, nil
}

func (l SessionLog) Runs(ctx context.Context) ([]repository.RunLocation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([]repository.RunLocation, 0, len(l.index.runs))
	for _, value := range l.index.runs {
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b repository.RunLocation) int { return a.Record.CreatedAt.Compare(b.Record.CreatedAt) })
	return values, nil
}

func (l SessionLog) Run(ctx context.Context, id identity.RunID) (repository.RunLocation, error) {
	if err := ctx.Err(); err != nil {
		return repository.RunLocation{}, err
	}
	value, ok := l.index.runs[id]
	if !ok {
		return repository.RunLocation{}, errorcode.ErrNotFound
	}
	return value, nil
}

func (l SessionLog) AgentHeads(ctx context.Context) ([]repository.AgentHead, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([]repository.AgentHead, 0, len(l.index.order))
	for agent, ids := range l.index.order {
		if len(ids) > 0 {
			values = append(values, repository.AgentHead{AgentID: agent, EntryID: ids[len(ids)-1]})
		}
	}
	return values, nil
}

func (l SessionLog) RecoveryRuns(ctx context.Context) ([]identity.RunID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []identity.RunID
	for id, run := range l.index.runs {
		incomplete := run.EndEntryID == ""
		for _, done := range l.index.calls[id] {
			incomplete = incomplete || !done
		}
		if incomplete {
			values = append(values, id)
		}
	}
	return values, nil
}

func (l SessionLog) RunEntries(ctx context.Context, id identity.RunID) ([]string, error) {
	run, err := l.Run(ctx, id)
	if err != nil {
		return nil, err
	}
	var values []string
	for _, entryID := range l.index.order[run.Record.AgentID] {
		if l.index.entries[entryID].Node.RunID == id.String() {
			values = append(values, entryID)
		}
	}
	return values, nil
}

func (l SessionLog) Requests(ctx context.Context, turnIDs []identity.TurnID) ([]sessionentry.ModelRequestRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make(map[identity.TurnID]bool, len(turnIDs))
	for _, id := range turnIDs {
		wanted[id] = true
	}
	var values []sessionentry.ModelRequestRecord
	for _, value := range l.index.requests {
		if wanted[value.TurnID] {
			values = append(values, value)
		}
	}
	return values, nil
}
