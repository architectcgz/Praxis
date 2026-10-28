package storage

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// contextRecord 保存最近一次完整压缩结果及其覆盖的事件边界。
type contextRecord struct {
	AgentID         identity.AgentID            `json:"agent_id"`
	ThroughSequence uint64                      `json:"through_sequence"`
	Entries         []sessionentry.ContextEntry `json:"entries"`
}

func (r contextRecord) Validate() error {
	if len(r.Entries) == 0 {
		return errors.New("saved context entries are required")
	}
	for _, entry := range r.Entries {
		if err := (entryRecord{AgentID: r.AgentID, Data: entry}).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func cloneContext(value contextRecord) contextRecord {
	value.Entries = (appcontext.ModelContext{Entries: value.Entries}).Clone().Entries
	return value
}

func (r EntryRepository) SaveContext(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, entries []sessionentry.ContextEntry) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	value := cloneContext(contextRecord{
		AgentID:         agentID,
		ThroughSequence: r.storage.state.sequences[sessionID.String()],
		Entries:         entries,
	})
	if err := r.storage.appendEventLocked(ctx, sessionID, "context.saved", "context", sessionID.String(), value); err != nil {
		return err
	}
	r.storage.state.contexts[sessionID] = value
	return nil
}

// LoadContext 读取快照和之后已准入的普通条目；只覆盖快照后修改且仍在快照中的回执。
func (r EntryRepository) LoadContext(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID) ([]sessionentry.ContextEntry, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	if _, exists := r.storage.state.sessions[sessionID]; !exists {
		return nil, errorcode.ErrNotFound
	}
	var entries []sessionentry.ContextEntry
	var boundary uint64
	if value, ok := r.storage.state.contexts[sessionID]; ok {
		value = cloneContext(value)
		entries, boundary = value.Entries, value.ThroughSequence
	}
	for _, key := range r.storage.state.entryOrder[sessionID] {
		stored := r.storage.state.entries[key]
		value := stored.Record.Data
		if key.AgentID != agentID || value.IsSummary() {
			continue
		}
		if value.Sequence <= boundary {
			if stored.UpdatedSequence > boundary {
				for index, entry := range entries {
					if entry.ID == value.ID {
						entries[index].Content = value.Clone().Content
						break
					}
				}
			}
			continue
		}
		if value.Role == role.User && value.ExecutionID != "" {
			if _, admitted := r.storage.state.executions[identity.AgentExecutionID(value.ExecutionID)]; !admitted {
				continue
			}
		}
		entries = append(entries, value.Clone())
	}
	return entries, nil
}
