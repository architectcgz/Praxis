package storage

import (
	"cmp"
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// contextRecord 保存最近一次完整压缩结果；后续 Entry 由其事件序号划定追加边界。
// 每个会话在存储内存中只保留最新值，原始 Entry 仍独立保留。
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

// SaveContext 在原始摘要已接纳后保存完整上下文，不修改任何原始 Entry。
// 保存失败时调用方不得替换会话持有的内存上下文。
func (r EntryRepository) SaveContext(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, entries []sessionentry.ContextEntry) error {
	return withState(ctx, r.storage, func(state *Storage, records []logRecord) error {
		value := contextRecord{
			AgentID:         agentID,
			ThroughSequence: state.sequences[sessionID.String()],
			Entries:         entries,
		}
		if err := value.Validate(); err != nil {
			return err
		}
		return appendRecord(state, records, sessionID.String(), "context", sessionID.String(), "context.saved", value)
	})
}

// LoadContext 只读取最近保存的压缩结果和之后已准入的普通条目，不重放摘要替代关系。
// 摘要追加与上下文保存之间若中断，未进入保存结果的摘要不生效，原文仍可继续使用。
func (r EntryRepository) LoadContext(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID) (entries []sessionentry.ContextEntry, err error) {
	err = withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		scope := sessionID.String()
		if err := requireMain(records, scope, agentID); err != nil {
			return err
		}
		boundary := uint64(0)
		for _, record := range records {
			if record.scope != scope || record.event.Collection != "context" {
				continue
			}
			value := cloneRecordValue(record.value).(contextRecord)
			if value.AgentID != agentID {
				return errorcode.ErrRequestConflict
			}
			entries, boundary = value.Entries, record.event.Sequence
			break
		}
		var appended []sessionentry.ContextEntry
		for _, record := range records {
			if record.scope != scope || record.event.Collection != "entry" || record.event.Sequence <= boundary {
				continue
			}
			value := record.value.(entryRecord)
			if value.AgentID != agentID || value.Data.IsSummary() {
				continue
			}
			// 更新不改变原条目顺序；只覆盖快照中仍然存在的回执，不恢复被压缩掉的条目。
			if value.Data.Sequence <= boundary {
				for index, entry := range entries {
					if entry.ID == value.Data.ID {
						entries[index].Content = value.Data.Clone().Content
						break
					}
				}
				continue
			}
			if value.Data.Role == role.User && value.Data.ExecutionID != "" {
				if _, err := findRecord(records, "agent_execution", value.Data.ExecutionID); errors.Is(err, errorcode.ErrNotFound) {
					continue
				} else if err != nil {
					return err
				}
			}
			appended = append(appended, value.Data.Clone())
		}
		sorted(appended, func(a, b sessionentry.ContextEntry) int { return cmp.Compare(a.Sequence, b.Sequence) })
		entries = append(entries, appended...)
		return nil
	})
	return entries, err
}
