package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"strings"
	"time"
)

// entryRecord 保存唯一 Entry 正文，Session 归属来自文件 Scope。
type entryRecord struct {
	AgentID identity.AgentID          `json:"agent_id"`
	Data    sessionentry.ContextEntry `json:"entry"`
}

type EntryRepository struct{ storage *Storage }

func entryKey(agentID identity.AgentID, id string) string {
	return fmt.Sprintf("%d:%s%s", len(agentID), agentID, id)
}

type storedEntry struct {
	scope string
	entry entryRecord
}

func findEntry(records []logRecord, agentID identity.AgentID, id string) (storedEntry, error) {
	for _, record := range records {
		if record.event.Collection != "entry" || record.event.Key != entryKey(agentID, id) {
			continue
		}
		value := cloneRecordValue(record.value).(entryRecord)
		return storedEntry{scope: record.scope, entry: value}, nil
	}
	return storedEntry{}, errorcode.ErrNotFound
}

func (r EntryRepository) Append(ctx context.Context, value sessionentry.Record) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		scope := value.SessionID.String()
		if err := requireMain(records, scope, value.AgentID); err != nil {
			return err
		}
		prior, readErr := findEntry(records, value.AgentID, value.Data.ID)
		if readErr == nil {
			candidate := value.Data.Clone()
			candidate.Sequence, candidate.CreatedAt = prior.entry.Data.Sequence, prior.entry.Data.CreatedAt
			if prior.scope != scope || !same(prior.entry.Data, candidate) {
				return errorcode.ErrRequestConflict
			}
			stored = prior.entry.Data.Clone()
			return nil
		}
		if !errors.Is(readErr, errorcode.ErrNotFound) {
			return readErr
		}
		data := value.Data.Clone()
		data.Sequence = state.sequences[scope] + 1
		if data.CreatedAt.IsZero() {
			data.CreatedAt = time.Now().UTC()
		}
		record := entryRecord{AgentID: value.AgentID, Data: data}
		if err := appendRecord(state, records, scope, "entry", entryKey(value.AgentID, data.ID), "entry.appended", record); err != nil {
			return err
		}
		stored, inserted = data.Clone(), true
		return nil
	})
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

func (r EntryRepository) Get(ctx context.Context, agentID identity.AgentID, id string) (result sessionentry.ContextEntry, err error) {
	err = withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		value, err := findEntry(records, agentID, id)
		if err != nil {
			return err
		}
		if err := requireMain(records, value.scope, agentID); err != nil {
			return err
		}
		result = value.entry.Data.Clone()
		return nil
	})
	return result, err
}

func (r EntryRepository) List(ctx context.Context, query repository.EntryQuery) (page repository.EntryList, err error) {
	err = withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		page.Entries = make([]sessionentry.ContextEntry, 0)
		for _, record := range records {
			if record.event.Collection != "entry" {
				continue
			}
			value := record.value.(entryRecord)
			if value.AgentID != query.AgentID {
				continue
			}
			if err := requireMain(records, record.scope, query.AgentID); err != nil {
				return err
			}
			if value.Data.Role == role.User && !query.IncludeUnadmitted && value.Data.ExecutionID != "" {
				if _, err := findRecord(records, "agent_execution", value.Data.ExecutionID); errors.Is(err, errorcode.ErrNotFound) {
					continue
				} else if err != nil {
					return err
				}
			}
			page.Entries = append(page.Entries, value.Data.Clone())
			page.SequenceBoundary = state.sequences[record.scope]
		}
		sorted(page.Entries, func(a, b sessionentry.ContextEntry) int { return cmp.Compare(a.Sequence, b.Sequence) })
		return nil
	})
	return page, err
}

// Validate 只在磁盘恢复输入边界检查消息结构及 Agent 身份，不修正已提交内容。
func (r entryRecord) Validate() error {
	if r.AgentID == "" || r.AgentID.String() != strings.TrimSpace(r.AgentID.String()) || strings.ContainsAny(r.AgentID.String(), "\x00\r\n") {
		return errors.New("message agent identity must be canonical and nonempty")
	}
	if r.Data.Role == role.Assistant && (r.Data.AuthorKind != sessionentry.AuthorAgent || r.Data.AuthorID != r.AgentID.String()) {
		return errors.New("assistant author does not match the message agent")
	}
	return r.Data.Validate()
}
