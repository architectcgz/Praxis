package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	workflowmodel "praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/timing"
)

func describe(scope string, event Event) (entityKey, indexEntry, error) {
	key := eventKey(scope, event)
	entry := indexEntry{location: recordRef{Scope: scope}, groups: []lookupKey{collectionKey(event.Collection), scopeKey(scope)}}
	group := func(kind, owner, value string) { entry.groups = append(entry.groups, lookupKey{kind, owner, value}) }
	claim := func(kind, owner, value string) { entry.claims = append(entry.claims, lookupKey{kind, owner, value}) }
	link := func(collection, id string) {
		if id != "" {
			entry.groups = append(entry.groups, referenceKey(entityKey{collection, id}))
		}
	}
	kinds := map[string]string{
		"project":   "project.saved",
		"workspace": "workspace.saved",
		"session":   "session.saved",
		"agent":     "agent.saved",
		"task":      "task.saved",
		"tool":      "tool.saved",
		"wait":      "wait.saved",
		"control":   "control.saved",
		"context":   "context.entry_appended",
		"message":   "message.appended",
		"timing":    "timing.saved",
		"usage":     "usage.saved",
	}
	if kinds[event.Collection] == "" || kinds[event.Collection] != event.Type {
		return key, entry, errors.New("unknown event type")
	}
	if (scope == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return key, entry, errors.New("invalid event file ownership")
	}
	if event.Key == "" || event.Key != strings.TrimSpace(event.Key) {
		return key, entry, errors.New("invalid object key")
	}
	var object interface{ Validate() error }
	var identity, sessionID string
	decode := func(target any) error { return decodeJSON(event.Payload, target) }
	switch event.Collection {
	case "project":
		var v projectmodel.Project
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity = v, v.ID.String()
		link("workspace", v.DefaultWorkspaceID.String())
	case "workspace":
		var v workspacemodel.Workspace
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity = v, v.ID.String()
		group("workspaces-project", v.ProjectID.String(), "")
		claim("workspace-path", v.ProjectID.String(), v.Path)
		link("project", v.ProjectID.String())
	case "session":
		var v sessionmodel.Session
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), v.ID.String()
		group("sessions-project", v.ProjectID.String(), "")
		link("workspace", v.WorkspaceID.String())
	case "agent":
		var v agentmodel.Agent
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), v.SessionID.String()
		group("agents-session", sessionID, "")
		group("agents-definition", sessionID, v.DefinitionID.String())
		if v.DefinitionID == agentmodel.DefinitionPrimary {
			claim("primary-agent", sessionID, "")
		}
		link("session", sessionID)
		link("task", v.CurrentTaskID.String())
	case "task":
		var v taskmodel.Task
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), v.SessionID.String()
		entry.sequence = v.Sequence
		group("tasks-agent", v.AgentID.String(), "")
		claim("task-request", v.AgentID.String(), v.RequestID.String())
		if v.Sequence != 0 {
			claim("task-sequence", v.AgentID.String(), fmt.Sprint(v.Sequence))
		}
		if v.Active() {
			claim("active-agent", v.AgentID.String(), "")
			group("active-session", sessionID, "")
			group("active-tasks", "", "")
		}
		if v.Status == taskmodel.TaskPending {
			group("pending-agent", v.AgentID.String(), "")
		}
		link("agent", v.AgentID.String())
	case "tool":
		var v toolmodel.ToolInvocation
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), v.SessionID.String()
		claim("tool-call", v.TaskID.String(), v.ProviderToolCallID)
		group("tools-task", v.TaskID.String(), "")
		if !v.Status.Terminal() {
			group("unsettled-session", sessionID, "")
		}
		if v.Status == toolmodel.ToolInvocationAwaitingApproval {
			group("awaiting-agent", v.AgentID.String(), "")
		}
		link("task", v.TaskID.String())
	case "context":
		var v sessionmodel.SessionContextEntry
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), v.SessionID.String()
		group("contexts-session", sessionID, "")
		link("session", sessionID)
		link("task", v.SourceTaskID.String())
	case "control":
		var v agentmodel.AgentControlCommand
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), scope
		if v.Status == agentmodel.AgentControlPending {
			group("controls-agent", v.AgentID.String(), "")
		}
		link("agent", v.AgentID.String())
		link("task", v.TargetTaskID.String())
	case "wait":
		var v workflowmodel.WaitCondition
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID.String(), scope
		link("task", v.TaskID.String())
	case "message":
		var v messageRecord
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v.Data, messageKey(v.OwnerID, v.Data.ID), scope
		if v.Data.Sequence != event.Sequence {
			return key, entry, errors.New("message sequence does not match event sequence")
		}
		// 消息不进入全局 latest / groups，正文与位置列表均按 Session 懒加载。
		entry.groups = nil
	case "usage":
		var v model.ModelUsageRecord
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.Key(), v.SessionID
		group("usage-session", sessionID, "")
		link("task", v.TaskID)
	case "timing":
		var v timing.Record
		if err := decode(&v); err != nil {
			return key, entry, err
		}
		object, identity, sessionID = v, v.ID, v.SessionID
		group("timings-agent", v.AgentID, "")
		if v.Status == timing.Running {
			group("running-timings", "", "")
		}
		link("task", v.TaskID)
	}
	if identity != event.Key || sessionID != scope {
		return key, entry, errors.New("object identity or session ownership does not match event")
	}
	if err := object.Validate(); err != nil {
		return key, entry, err
	}
	// 持久化只接受 canonical 身份；业务正文与提示词不在这里重复规范化。
	value := reflect.ValueOf(object)
	for _, field := range []string{
		"ID",
		"SessionID",
		"AgentID",
		"TaskID",
		"RequestID",
		"TurnID",
		"InputMessageID",
		"ProviderID",
		"ModelID",
		"ReasoningLevel",
	} {
		v := value.FieldByName(field)
		if v.IsValid() && v.Kind() == reflect.String && v.String() != strings.TrimSpace(v.String()) {
			return key, entry, fmt.Errorf("%s must be canonical", field)
		}
	}
	return key, entry, nil
}

func (s *Store) validateTransaction(ctx context.Context) error {
	tx := s.tx(ctx)
	if tx.deletedScope {
		// 删除检查最终事务状态，但不能因为即将清理索引而隐藏仍在运行的 Task。
		tx.deletedScope = false
		_, err := s.payload(ctx, entityKey{"session", tx.scope})
		active := len(s.keys(ctx, lookupKey{kind: "active-session", owner: tx.scope}))
		tx.deletedScope = true
		if err != nil {
			return err
		}
		if active != 0 {
			return contracts.ErrAgentExecuting
		}
		return nil
	}
	for key, change := range tx.changes {
		for _, claim := range change.entry.claims {
			if old, ok := s.index.unique[claim]; ok && old != key {
				if next, changed := tx.changes[old]; !changed || slices.Contains(next.entry.claims, claim) {
					return contracts.ErrRequestConflict
				}
			}
			for other, next := range tx.changes {
				if other != key && slices.Contains(next.entry.claims, claim) {
					return contracts.ErrRequestConflict
				}
			}
		}
		if err := s.validateRelations(ctx, key, change.entry); err != nil {
			return fmt.Errorf("%s/%s: %w", key.collection, key.id, err)
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
	return s.prepareReceiptChanges(ctx)
}

func (s *Store) validateRelations(ctx context.Context, key entityKey, entry indexEntry) error {
	data, err := s.payload(ctx, key)
	if err != nil {
		return err
	}
	scope := entry.location.Scope
	reference := func(collection, id string) error {
		_, err := s.payload(ctx, entityKey{collection, id})
		if err != nil {
			return err
		}
		if collection == "workspace" || collection == "project" {
			return nil
		}
		ref, ok := s.entry(ctx, entityKey{collection, id})
		if !ok || ref.location.Scope != scope {
			return errors.New("reference ownership mismatch")
		}
		return nil
	}
	taskOwner := func(id contracts.TaskID, agent contracts.AgentID) error {
		value, err := get[taskmodel.Task](ctx, s, "task", id.String())
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
		if err != nil && !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		if (err == nil && active.id != v.CurrentTaskID.String()) || (err != nil && v.CurrentTaskID != "") {
			return errors.New("agent does not match active task")
		}
	case "task":
		var v taskmodel.Task
		_ = decodeJSON(data, &v)
		agent, err := get[agentmodel.Agent](ctx, s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if agent.SessionID != v.SessionID || v.Active() != (agent.CurrentTaskID == v.ID) {
			return errors.New("task does not match agent active identity")
		}
	case "tool":
		var v toolmodel.ToolInvocation
		_ = decodeJSON(data, &v)
		return taskOwner(v.TaskID, v.AgentID)
	case "context":
		var v sessionmodel.SessionContextEntry
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		if v.SourceTaskID != "" {
			return reference("task", v.SourceTaskID.String())
		}
	case "control":
		var v agentmodel.AgentControlCommand
		_ = decodeJSON(data, &v)
		if err := reference("agent", v.AgentID.String()); err != nil {
			return err
		}
		return taskOwner(v.TargetTaskID, v.AgentID)
	case "wait":
		var v workflowmodel.WaitCondition
		_ = decodeJSON(data, &v)
		return taskOwner(v.TaskID, v.AgentID)
	case "usage":
		var v model.ModelUsageRecord
		_ = decodeJSON(data, &v)
		return taskOwner(contracts.TaskID(v.TaskID), contracts.AgentID(v.AgentID))
	case "timing":
		var v timing.Record
		_ = decodeJSON(data, &v)
		return taskOwner(contracts.TaskID(v.TaskID), contracts.AgentID(v.AgentID))
	case "message":
		var v messageRecord
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		if v.OwnerID != "" {
			if err := reference("agent", v.OwnerID); err != nil {
				return err
			}
		}
		if v.Data.TaskID != "" {
			if err := reference("task", v.Data.TaskID); err != nil {
				return err
			}
			task, err := get[taskmodel.Task](ctx, s, "task", v.Data.TaskID)
			if err != nil {
				return err
			}
			agent, err := get[agentmodel.Agent](ctx, s, "agent", task.AgentID.String())
			if err != nil {
				return err
			}
			if appendOwner(agent) != v.OwnerID || v.Data.RequestID != task.RequestID.String() {
				return errors.New("message stream or request ownership mismatch")
			}
			if v.Data.Role == sessionmodel.RoleAssistant && v.Data.AuthorID != task.AgentID.String() {
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
	case "task":
		var prior, next taskmodel.Task
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if err := taskmodel.ValidateUpdate(prior, next); err != nil {
			return fmt.Errorf("%w: %v", contracts.ErrRequestConflict, err)
		}
		return nil
	case "tool":
		var prior, next toolmodel.ToolInvocation
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if err := toolmodel.ValidateUpdate(prior, next); err != nil {
			return fmt.Errorf("%w: %v", contracts.ErrRequestConflict, err)
		}
		return nil
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
		var prior, next model.ModelUsageRecord
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if next.Usage.InputTokens < prior.Usage.InputTokens {
			return contracts.ErrRequestConflict
		}
		for _, counts := range [][2]*int64{
			{prior.Usage.OutputTokens, next.Usage.OutputTokens},
			{prior.Usage.CacheReadInputTokens, next.Usage.CacheReadInputTokens},
			{prior.Usage.CacheCreationInputTokens, next.Usage.CacheCreationInputTokens},
		} {
			if counts[0] != nil && (counts[1] == nil || *counts[1] < *counts[0]) {
				return contracts.ErrRequestConflict
			}
		}
		fields = []string{"agentId", "taskId", "turnId"}
	case "timing":
		var prior, next timing.Record
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if next.Revision < prior.Revision || next.Revision == prior.Revision && !same(prior, next) || prior.Status != timing.Running && !same(prior, next) || prior.Operation != next.Operation || prior.ParentID != next.ParentID || !prior.StartedAt.Equal(next.StartedAt) {
			return contracts.ErrRequestConflict
		}
	case "context", "message":
		if !bytes.Equal(before, after) {
			return contracts.ErrRequestConflict
		}
	}
	for _, field := range fields {
		if !bytes.Equal(left[field], right[field]) {
			return fmt.Errorf("%w: %s.%s is immutable", contracts.ErrRequestConflict, collection, field)
		}
	}
	return nil
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }
func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
