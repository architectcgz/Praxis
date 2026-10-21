package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	messagemodel "praxis/internal/core/message"
	modelapi "praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	queued "praxis/internal/core/task"
	timingmodel "praxis/internal/core/timing"
	"praxis/internal/core/toolexecution"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/workflow"
	"slices"
)

// prepareCommit 校验最终 overlay 的唯一性和消息配对，不编排业务状态。
func (s *Store) prepareCommit(ctx context.Context) error {
	tx := s.tx(ctx)
	if tx.deletedScope {
		return nil
	}
	claimed := make(map[lookupKey]entityKey)
	for key, change := range tx.changes {
		if key.collection == "message" {
			if err := s.validateRelations(ctx, key, change.entry); err != nil {
				return err
			}
		}
		// Scope 引用与日志身份是结构约束；Session 活跃状态由 Service 检查。
		if change.entry.scope != "" {
			if _, ok := s.entry(ctx, entityKey{"session", change.entry.scope}); !ok {
				return fault.ErrNotFound
			}
		}
		if key.collection == "session" {
			if _, exists := s.state.entries[key]; !exists && s.log.Sequence(change.entry.scope) != 0 {
				return errors.New("session identity cannot be reused")
			}
		}
		for _, claim := range change.entry.claims {
			if _, exists := claimed[claim]; exists {
				return fault.ErrRequestConflict
			}
			claimed[claim] = key
			if old, exists := s.state.unique[claim]; exists && old != key {
				if next, changed := tx.changes[old]; !changed || slices.Contains(next.entry.claims, claim) {
					return fault.ErrRequestConflict
				}
			}
		}
	}

	return s.validateToolReceipts(ctx)
}

// validateRestoredTransaction 只在磁盘恢复边界检查业务关联，拒绝不一致的日志。
func (s *Store) validateRestoredTransaction(ctx context.Context) error {
	tx := s.tx(ctx)
	if tx.deletedScope {
		// 删除检查最终事务状态，但不能因为即将清理索引而隐藏仍在运行的 Execution。
		tx.deletedScope = false
		_, err := s.payload(ctx, entityKey{"session", tx.scope})
		active := len(s.keys(ctx, lookupKey{kind: "active-session", owner: tx.scope}))
		tx.deletedScope = true
		if err != nil {
			return err
		}
		if active != 0 {
			return agentmodel.ErrAgentExecuting
		}
		return nil
	}
	for key, change := range tx.changes {
		if key.collection != "message" {
			if err := s.validateRelations(ctx, key, change.entry); err != nil {
				return fmt.Errorf("%s/%s: %w", key.collection, key.id, err)
			}
		}
		// Workspace.ProjectID 可变，需同时校验引用它的 Project / Session。
		if key.collection == "workspace" {
			for _, dependent := range s.keys(ctx, referenceKey(key)) {
				entry, _ := s.entry(ctx, dependent)
				if err := s.validateRelations(ctx, dependent, entry); err != nil {
					return err
				}
			}
		}
	}
	return s.prepareCommit(ctx)
}

func (s *Store) validateRelations(ctx context.Context, key entityKey, entry stateEntry) error {
	data, err := s.payload(ctx, key)
	if err != nil {
		return err
	}
	scope := entry.scope
	reference := func(collection, id string) error {
		_, err := s.payload(ctx, entityKey{collection, id})
		if err != nil {
			return err
		}
		if collection == "workspace" || collection == "project" {
			return nil
		}
		ref, ok := s.entry(ctx, entityKey{collection, id})
		if !ok || ref.scope != scope {
			return errors.New("reference ownership mismatch")
		}
		return nil
	}
	taskOwner := func(id identity.AgentExecutionID, agent identity.AgentID) error {
		value, err := get[agentmodel.Execution](ctx, s, "agent_execution", id.String())
		if err != nil {
			return err
		}
		if value.SessionID.String() != scope || value.AgentID != agent {
			return errors.New("task and agent ownership mismatch")
		}
		return nil
	}
	switch key.collection {
	case "project":
		var v projectmodel.Project
		_ = decodeJSON(data, &v)
		workspace, err := get[workspacemodel.Workspace](ctx, s, "workspace", v.DefaultWorkspaceID.String())
		if err != nil {
			return err
		}
		if workspace.ProjectID != v.ID {
			return errors.New("project default workspace ownership mismatch")
		}
	case "workspace":
		var v workspacemodel.Workspace
		_ = decodeJSON(data, &v)
		return reference("project", v.ProjectID.String())
	case "session":
		var v sessionmodel.Session
		_ = decodeJSON(data, &v)
		if !jsonl.ValidScope(v.ProjectID.String()) {
			return errors.New("session project ID is not a valid directory name")
		}
		if projectID := s.log.Project(scope); projectID != "" && projectID != v.ProjectID.String() {
			return errors.New("session project does not match log directory")
		}
		workspace, err := get[workspacemodel.Workspace](ctx, s, "workspace", v.WorkspaceID.String())
		if err != nil {
			return err
		}
		if workspace.ProjectID != v.ProjectID {
			return errors.New("session workspace ownership mismatch")
		}
	case "agent":
		var v agentmodel.Agent
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		active, err := s.lookup(ctx, lookupKey{kind: "active-agent", owner: v.ID.String()})
		if err != nil && !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		if (err == nil && active.id != v.CurrentExecutionID.String()) || (err != nil && v.CurrentExecutionID != "") {
			return errors.New("agent does not match active task")
		}
	case "agent_execution":
		var v agentmodel.Execution
		_ = decodeJSON(data, &v)
		agent, err := get[agentmodel.Agent](ctx, s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if agent.SessionID != v.SessionID || v.Active() != (agent.CurrentExecutionID == v.ID) {
			return errors.New("execution does not match agent active identity")
		}
		if v.SourceTaskID != "" {
			pending, err := get[queued.Task](ctx, s, "task", v.SourceTaskID.String())
			if err != nil {
				return err
			}
			if pending.Status != queued.Dispatched || pending.ExecutionID != v.ID || pending.AgentID != v.AgentID || pending.RequestID != v.RequestID {
				return errors.New("execution source task mismatch")
			}
		}
	case "task":
		var v queued.Task
		_ = decodeJSON(data, &v)
		owner, err := get[agentmodel.Agent](ctx, s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if owner.SessionID != v.SessionID {
			return errors.New("queued task ownership mismatch")
		}
		if v.Status == queued.Dispatched {
			execution, err := get[agentmodel.Execution](ctx, s, "agent_execution", v.ExecutionID.String())
			if err != nil {
				return err
			}
			if execution.SourceTaskID != v.ID || execution.AgentID != v.AgentID || execution.RequestID != v.RequestID || execution.InputMessageID != v.InputMessageID {
				return errors.New("queued task execution mismatch")
			}
		}
	case "tool_event":
		var v toolexecution.Event
		_ = decodeJSON(data, &v)
		call, err := (ToolCallQueries{s}).GetCall(ctx, v.Key)
		if err != nil {
			return err
		}
		if v.Kind == toolexecution.Approved || v.Kind == toolexecution.ApprovalRequired {
			if toolexecution.ArgumentsDigest(call.Name, []byte(v.Summary)) != v.ArgumentsDigest {
				return fault.ErrRequestConflict
			}
		}
		if call.SessionID.String() != scope || v.At.Before(call.CreatedAt) {
			return fault.ErrRequestConflict
		}
		_, err = (ToolExecutionRepository{s}).Read(ctx, v.Key)
		return err
	case "context":
		var v sessionmodel.SessionContextEntry
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		if v.SourceExecutionID != "" {
			return reference("agent_execution", v.SourceExecutionID.String())
		}
	case "control":
		var v agentmodel.AgentControlCommand
		_ = decodeJSON(data, &v)
		if err := reference("agent", v.AgentID.String()); err != nil {
			return err
		}
		return taskOwner(v.TargetExecutionID, v.AgentID)
	case "wait":
		var v workflow.WaitCondition
		_ = decodeJSON(data, &v)
		return taskOwner(v.ExecutionID, v.AgentID)
	case "usage":
		var v modelapi.ModelUsageRecord
		_ = decodeJSON(data, &v)
		return taskOwner(identity.AgentExecutionID(v.ExecutionID), identity.AgentID(v.AgentID))
	case "timing":
		var v timingmodel.Record
		_ = decodeJSON(data, &v)
		return taskOwner(identity.AgentExecutionID(v.ExecutionID), identity.AgentID(v.AgentID))
	case "message":
		var v messageRecord
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		agent, err := get[agentmodel.Agent](ctx, s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if agent.SessionID.String() != scope || v.OwnerID != "" && v.OwnerID != v.AgentID.String() {
			return errors.New("message agent does not match the stream ownership")
		}
		if v.Data.SourceTaskID != "" {
			pending, err := get[queued.Task](ctx, s, "task", v.Data.SourceTaskID)
			if err != nil {
				return err
			}
			if pending.AgentID != v.AgentID || pending.SessionID.String() != scope || pending.RequestID.String() != v.Data.RequestID || pending.InputMessageID != v.Data.ID || v.Data.Role != messagemodel.RoleUser || v.Data.ExecutionID != "" {
				return errors.New("queued message ownership mismatch")
			}
		}
		if v.Data.ExecutionID != "" {
			if err := reference("agent_execution", v.Data.ExecutionID); err != nil {
				return err
			}
			task, err := get[agentmodel.Execution](ctx, s, "agent_execution", v.Data.ExecutionID)
			if err != nil {
				return err
			}
			agent, err := get[agentmodel.Agent](ctx, s, "agent", task.AgentID.String())
			if err != nil {
				return err
			}
			if agent.ID != v.AgentID || v.Data.RequestID != task.RequestID.String() {
				return errors.New("message stream or request ownership mismatch")
			}
			if v.Data.Role == messagemodel.RoleAssistant && v.Data.AuthorID != task.AgentID.String() {
				return errors.New("assistant author ownership mismatch")
			}
		}
	}
	return nil
}
func immutable(collection string, before, after json.RawMessage) error {
	var left, right map[string]json.RawMessage
	if err := json.Unmarshal(before, &left); err != nil {
		return err
	}
	if err := json.Unmarshal(after, &right); err != nil {
		return err
	}
	var fields []string
	switch collection {
	case "agent_execution":
		var prior, next agentmodel.Execution
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if err := agentmodel.ValidateExecutionUpdate(prior, next); err != nil {
			return fmt.Errorf("%w: %v", fault.ErrRequestConflict, err)
		}
		return nil
	case "task":
		var prior, next queued.Task
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		return queued.ValidateUpdate(prior, next)
	case "agent":
		fields = []string{
			"ID",
			"DefinitionID",
			"Profile",
			"CreatedAt",
		}
	case "session":
		fields = []string{"ProjectID", "WorkspaceID", "CreatedAt"}
	case "usage":
		var prior, next modelapi.ModelUsageRecord
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if next.Usage.InputTokens < prior.Usage.InputTokens {
			return fault.ErrRequestConflict
		}
		for _, counts := range [][2]*int64{
			{prior.Usage.OutputTokens, next.Usage.OutputTokens},
			{prior.Usage.CacheReadInputTokens, next.Usage.CacheReadInputTokens},
			{prior.Usage.CacheCreationInputTokens, next.Usage.CacheCreationInputTokens},
		} {
			if counts[0] != nil && (counts[1] == nil || *counts[1] < *counts[0]) {
				return fault.ErrRequestConflict
			}
		}
		fields = []string{"agentId", "executionId", "turnId"}
	case "timing":
		var prior, next timingmodel.Record
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if next.Revision < prior.Revision || next.Revision == prior.Revision && !same(prior, next) || prior.Status != timingmodel.Running && !same(prior, next) || prior.Operation != next.Operation || prior.ParentID != next.ParentID || !prior.StartedAt.Equal(next.StartedAt) {
			return fault.ErrRequestConflict
		}
	case "context", "message", "tool_event":
		if !bytes.Equal(before, after) {
			return fault.ErrRequestConflict
		}
	}
	for _, field := range fields {
		if !bytes.Equal(left[field], right[field]) {
			return fmt.Errorf("%w: %s.%s is immutable", fault.ErrRequestConflict, collection, field)
		}
	}
	return nil
}
