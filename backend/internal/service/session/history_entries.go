package session

import (
	"context"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"time"
)

// entryRecord 保存唯一 Entry 正文，会话归属由所在日志文件确定。
type entryRecord struct {
	AgentID identity.AgentID          `json:"agent_id"`
	Data    sessionentry.ContextEntry `json:"entry"`
}

// contextRecord 保存最近一次完整压缩结果及其覆盖的事件边界。
type contextRecord struct {
	AgentID         identity.AgentID            `json:"agent_id"`
	ThroughSequence uint64                      `json:"through_sequence"`
	Entries         []sessionentry.ContextEntry `json:"entries"`
}

func encodedEntryKey(agentID identity.AgentID, id string) string {
	return fmt.Sprintf("%d:%s%s", len(agentID), agentID, id)
}

// appendEntryLocked 供正常追加和持锁恢复修复共用，成功写入完整记录后才修改内存态。
func (h *sessionHistory) appendEntryLocked(ctx context.Context, value entryRecord) (sessionentry.ContextEntry, bool, error) {
	if index := h.state.entryIndex(value.AgentID, value.Data.ID); index >= 0 {
		prior := h.state.entries[index].Data
		candidate := value.Data.Clone()
		candidate.Sequence, candidate.CreatedAt = prior.Sequence, prior.CreatedAt
		if !reflect.DeepEqual(prior, candidate) {
			return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
		}
		return prior.Clone(), false, nil
	}
	data := value.Data.Clone()
	data.Sequence = h.state.sequence + 1
	if data.CreatedAt.IsZero() {
		data.CreatedAt = time.Now().UTC()
	}
	record := entryRecord{AgentID: value.AgentID, Data: data}
	if err := h.appendEventLocked(ctx, "entry.appended", "entry", encodedEntryKey(value.AgentID, data.ID), record); err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	h.state.entries = append(h.state.entries, record)
	return data.Clone(), true, nil
}

func (h *sessionHistory) entryLocked(id string) (sessionentry.ContextEntry, error) {
	index := h.state.entryIndex(h.state.session.MainAgentID(), id)
	if index < 0 {
		return sessionentry.ContextEntry{}, errorcode.ErrNotFound
	}
	return h.state.entries[index].Data.Clone(), nil
}

func (h *sessionHistory) listEntriesLocked(includeUnadmitted bool) sessionentry.List {
	page := sessionentry.List{
		SequenceBoundary: h.state.sequence,
		Entries:          make([]sessionentry.ContextEntry, 0, len(h.state.entries)),
	}
	for _, stored := range h.state.entries {
		if stored.AgentID != h.state.session.MainAgentID() {
			continue
		}
		value := stored.Data
		if value.Role == role.User && !includeUnadmitted && value.ExecutionID != "" && h.state.executionIndex(identity.AgentExecutionID(value.ExecutionID)) < 0 {
			continue
		}
		page.Entries = append(page.Entries, value.Clone())
	}
	return page
}

func (r entryRecord) Validate() error {
	return (sessionentry.Record{AgentID: r.AgentID, Data: r.Data}).Validate()
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
