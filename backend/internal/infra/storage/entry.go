package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"strings"
	"time"
)

// entryRecord 保存唯一 Entry 正文，Session 归属来自文件 Scope。
type entryRecord struct {
	OwnerID string                    `json:"owner_id,omitempty"`
	AgentID identity.AgentID          `json:"agent_id"`
	Data    sessionentry.ContextEntry `json:"entry"`
}

type EntryRepository struct{ s *Store }

func entryKey(owner, id string) string { return fmt.Sprintf("%d:%s%s", len(owner), owner, id) }
func entryEntityKey(scope, owner, id string) entityKey {
	return entityKey{"entry", scope + "\x00" + entryKey(owner, id)}
}

func (r EntryRepository) Append(ctx context.Context, value sessionentry.Record) (stored sessionentry.ContextEntry, inserted bool, err error) {
	scope, owner := value.Stream.SessionID.String(), value.Stream.OwnerAgentID.String()
	if scope == "" || value.AgentID == "" || owner != "" && owner != value.AgentID.String() {
		return stored, false, errors.New("message stream ownership is required")
	}
	err = r.s.view(ctx, func() error {
		if err := r.s.flushLocked(ctx); err != nil {
			return err
		}
		key := entryEntityKey(scope, owner, value.Data.ID)
		if prior, exists := r.s.entry(key); exists {
			record := prior.entryValue()
			candidate := value.Data
			candidate.Sequence, candidate.CreatedAt = record.Data.Sequence, record.Data.CreatedAt
			if record.AgentID != value.AgentID || !same(record.Data, candidate) {
				return fault.ErrRequestConflict
			}
			stored = record.Data
			return nil
		}

		// 幂等回读先于状态检查；新输出必须在当前执行仍允许写入时提交。
		if value.Data.Role == role.Assistant {
			execution, err := getLocked[agentmodel.Execution](r.s, "agent_execution", value.Data.ExecutionID)
			if err != nil {
				return err
			}
			if !execution.Active() || execution.Progress.FinalAssistantID != "" {
				return fault.ErrRequestConflict
			}
		}
		data := value.Data.Clone()
		data.Sequence = r.s.sequences[scope] + 1
		if data.CreatedAt.IsZero() {
			data.CreatedAt = time.Now().UTC()
		}
		record := entryRecord{OwnerID: owner, AgentID: value.AgentID, Data: data}
		if err := record.Validate(); err != nil {
			return err
		}
		if err := r.s.appendLocked(ctx, scope, "entry", entryKey(owner, data.ID), "entry.appended", record); err != nil {
			return err
		}
		stored, _ = r.s.history(value.Stream).Get(data.ID)
		inserted = true
		return nil
	})
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

func (r EntryRepository) Get(ctx context.Context, stream sessionentry.StreamKey, id string) (result sessionentry.ContextEntry, err error) {
	err = r.s.view(ctx, func() error {
		value, ok := r.s.history(stream).Get(id)
		if !ok {
			return fault.ErrNotFound
		}
		result = value
		return nil
	})
	return result, err
}

func (r EntryRepository) List(ctx context.Context, query repository.EntryQuery) (page repository.EntryList, err error) {
	err = r.s.view(ctx, func() error {
		page.Entries = make([]sessionentry.ContextEntry, 0)
		values := r.s.history(query.Stream).List()
		if query.ForContext {
			values = r.s.history(query.Stream).ContextHistory()
		}
		for _, value := range values {
			if value.Role == role.User && !query.IncludeUnadmitted && value.ExecutionID != "" {
				if _, exists := r.s.entry(entityKey{"agent_execution", value.ExecutionID}); !exists {
					continue
				}
			}
			page.Entries = append(page.Entries, value)
			page.SequenceBoundary = value.Sequence
		}
		return nil
	})
	return page, err
}

func (r EntryRepository) SetContext(ctx context.Context, stream sessionentry.StreamKey, value appcontext.ModelContext) (result *appcontext.ModelContext, err error) {
	err = r.s.view(ctx, func() error { result = r.s.history(stream).SetContext(value); return nil })
	return result, err
}

// Validate 只在磁盘读取入口检查消息结构和流身份。
func (r entryRecord) Validate() error {
	if r.AgentID == "" || r.AgentID.String() != strings.TrimSpace(r.AgentID.String()) || strings.ContainsAny(r.AgentID.String(), "\x00\r\n") {
		return errors.New("message agent identity must be canonical and nonempty")
	}
	if r.Data.Role == role.Assistant && (r.Data.AuthorKind != sessionentry.AuthorAgent || r.Data.AuthorID != r.AgentID.String()) {
		return errors.New("assistant author does not match the message agent")
	}
	if err := validateEntryData(r.Data); err != nil {
		return err
	}
	for _, block := range r.Data.Content {
		if block.Kind == sessionentry.BlockReasoningState {
			if err := validateReasoningState(block.ReasoningState); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateEntryData 在消息写入与磁盘恢复边界检查身份、角色及内容结构。
func validateEntryData(m sessionentry.ContextEntry) error {
	if m.ID == "" || m.Sequence == 0 || m.CreatedAt.IsZero() {
		return errors.New("message identity, sequence and creation time are required")
	}
	for _, value := range []string{
		m.ID,
		m.RequestID,
		m.SourceTaskID,
		m.HandoffID,
		m.ExecutionID,
		m.TurnID,
		m.AuthorID,
	} {
		if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("message identity fields must be canonical")
		}
	}
	switch m.Role {
	case role.User, role.Assistant, role.Tool:
	default:
		return errors.New("message role is invalid")
	}
	switch m.AuthorKind {
	case sessionentry.AuthorUser, sessionentry.AuthorAgent, sessionentry.AuthorTool, sessionentry.AuthorSystem:
	default:
		return errors.New("message author is invalid")
	}
	if m.IsSummary() {
		if m.Role != role.User || m.AuthorKind != sessionentry.AuthorSystem {
			return errors.New("summary entry requires user role and system author")
		}
	}
	if len(m.Content) == 0 && !(m.Role == role.Assistant && (m.StopReason == "completed" || m.StopReason == "stop_sequence")) {
		return errors.New("message blocks are required")
	}
	for _, block := range m.Content {
		if err := validateEntryBlock(m.Role, block); err != nil {
			return err
		}
	}
	return nil
}

func validateEntryBlock(messageRole role.Role, block sessionentry.Block) error {
	if block.Kind != sessionentry.BlockReasoningState && block.ReasoningState != nil {
		return errors.New("reasoning state requires a reasoning state message block")
	}
	switch block.Kind {
	case sessionentry.BlockReasoningState:
		if messageRole != role.Assistant || block.ReasoningState == nil || block.Text != "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid reasoning state block")
		}
	case sessionentry.BlockText:
		if strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid text block")
		}
	case sessionentry.BlockThinking:
		if messageRole != role.Assistant || strings.TrimSpace(block.Text) == "" || block.CallID != "" || block.Name != "" || len(block.Input) > 0 || block.IsError {
			return errors.New("message contains an invalid thinking block")
		}
	case sessionentry.BlockToolCall:
		if messageRole != role.Assistant || block.CallID == "" || block.Name == "" || block.CallID != strings.TrimSpace(block.CallID) || block.Name != strings.TrimSpace(block.Name) || block.IsError || len(block.Input) > 0 && !json.Valid(block.Input) {
			return errors.New("message contains an invalid tool call block")
		}
	case sessionentry.BlockToolResult:
		if messageRole != role.Tool || block.CallID == "" || block.Name == "" || block.CallID != strings.TrimSpace(block.CallID) || block.Name != strings.TrimSpace(block.Name) || len(block.Input) > 0 {
			return errors.New("message contains an invalid tool result block")
		}
	default:
		return errors.New("message contains an unknown block kind")
	}
	return nil
}
