package storage

import (
	"cmp"
	"context"
	"fmt"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"time"
)

// entryRecord 保存唯一 Entry 正文，Session 归属来自文件 Scope。
type entryRecord struct {
	AgentID identity.AgentID          `json:"agent_id"`
	Data    sessionentry.ContextEntry `json:"entry"`
}

// toolResultUpdate 保持正文更新的日志格式，身份和其他元数据沿用原条目。
type toolResultUpdate struct {
	AgentID identity.AgentID `json:"agent_id"`
	EntryID string           `json:"entry_id"`
	Text    string           `json:"text"`
}

type EntryRepository struct{ storage *Store }

func encodedEntryKey(key entryKey) string {
	return fmt.Sprintf("%d:%s%s", len(key.AgentID), key.AgentID, key.ID)
}

func (r EntryRepository) Append(ctx context.Context, value sessionentry.Record) (sessionentry.ContextEntry, bool, error) {
	if err := r.storage.lock(ctx); err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	defer r.storage.mu.Unlock()
	key := entryKey{AgentID: value.AgentID, ID: value.Data.ID}
	if prior, ok := r.storage.state.entries[key]; ok {
		candidate := value.Data.Clone()
		candidate.Sequence, candidate.CreatedAt = prior.Record.Data.Sequence, prior.Record.Data.CreatedAt
		if prior.SessionID != value.SessionID || !reflect.DeepEqual(prior.Record.Data, candidate) {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
		return prior.Record.Data.Clone(), false, nil
	}
	data := value.Data.Clone()
	data.Sequence = r.storage.state.sequences[value.SessionID.String()] + 1
	if data.CreatedAt.IsZero() {
		data.CreatedAt = time.Now().UTC()
	}
	record := entryRecord{AgentID: value.AgentID, Data: data}
	if err := r.storage.appendEventLocked(ctx, value.SessionID, "entry.appended", "entry", encodedEntryKey(key), record); err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	r.storage.state.entries[key] = storedEntry{
		SessionID:       value.SessionID,
		Record:          record,
		UpdatedSequence: data.Sequence,
	}
	r.storage.state.entryOrder[value.SessionID] = append(r.storage.state.entryOrder[value.SessionID], key)
	return data.Clone(), true, nil
}

// UpdateToolResult 只追加正文变更，原始条目在 entryOrder 中的位置不变。
func (r EntryRepository) UpdateToolResult(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, id, text string) (sessionentry.ContextEntry, error) {
	if err := r.storage.lock(ctx); err != nil {
		return sessionentry.ContextEntry{}, err
	}
	defer r.storage.mu.Unlock()
	key := entryKey{AgentID: agentID, ID: id}
	prior, ok := r.storage.state.entries[key]
	if !ok {
		return sessionentry.ContextEntry{}, errorcode.ErrNotFound
	}
	if prior.SessionID != sessionID {
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	// 更新目标必须支持正文修改；业务归属与大小限制由 Session 检查。
	if prior.Record.Data.Role != role.Tool || len(prior.Record.Data.Content) != 1 || prior.Record.Data.Content[0].Kind != sessionentry.BlockToolResult {
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	data := prior.Record.Data.Clone()
	if data.Content[0].Text == text {
		return data, nil
	}
	update := toolResultUpdate{AgentID: agentID, EntryID: id, Text: text}
	if err := r.storage.appendEventLocked(ctx, sessionID, "entry.updated", "entry", encodedEntryKey(key), update); err != nil {
		return sessionentry.ContextEntry{}, err
	}
	data.Content[0].Text = text
	prior.Record.Data = data
	prior.UpdatedSequence = r.storage.state.sequences[sessionID.String()]
	r.storage.state.entries[key] = prior
	return data.Clone(), nil
}

func (r EntryRepository) Get(ctx context.Context, agentID identity.AgentID, id string) (sessionentry.ContextEntry, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return sessionentry.ContextEntry{}, err
	}
	defer r.storage.mu.RUnlock()
	value, ok := r.storage.state.entries[entryKey{AgentID: agentID, ID: id}]
	if !ok {
		return sessionentry.ContextEntry{}, errorcode.ErrNotFound
	}
	return value.Record.Data.Clone(), nil
}

func (r EntryRepository) List(ctx context.Context, query sessionentry.Query) (sessionentry.List, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return sessionentry.List{}, err
	}
	defer r.storage.mu.RUnlock()
	page := sessionentry.List{Entries: make([]sessionentry.ContextEntry, 0)}
	for sessionID, keys := range r.storage.state.entryOrder {
		for _, key := range keys {
			if key.AgentID != query.AgentID {
				continue
			}
			value := r.storage.state.entries[key].Record.Data
			if value.Role == role.User && !query.IncludeUnadmitted && value.ExecutionID != "" {
				if _, admitted := r.storage.state.executions[identity.AgentExecutionID(value.ExecutionID)]; !admitted {
					continue
				}
			}
			page.Entries = append(page.Entries, value.Clone())
		}
		// 即使所有输入都处于准备阶段，查询边界仍来自同一次状态读取。
		if session, ok := r.storage.state.sessions[sessionID]; ok && session.MainAgentID == query.AgentID {
			page.SequenceBoundary = r.storage.state.sequences[sessionID.String()]
		}
	}
	sorted(page.Entries, func(a, b sessionentry.ContextEntry) int { return cmp.Compare(a.Sequence, b.Sequence) })
	return page, nil
}

// Validate 委托领域类型检查单个消息，不执行跨对象规则。
func (r entryRecord) Validate() error {
	return (sessionentry.Record{AgentID: r.AgentID, Data: r.Data}).Validate()
}
