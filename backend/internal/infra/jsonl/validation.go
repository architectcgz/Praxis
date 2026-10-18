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

	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_execution"
	workflowmodel "praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/timing"
)

// describe 直接从实体构建查询索引，不重新解析或校验业务输入。
func describe(scope string, event Event, object any) (entityKey, indexEntry, error) {
	key := eventKey(scope, event)
	entry := indexEntry{location: recordRef{Scope: scope}, groups: []lookupKey{collectionKey(event.Collection), scopeKey(scope)}}
	group := func(kind, owner, value string) { entry.groups = append(entry.groups, lookupKey{kind, owner, value}) }
	claim := func(kind, owner, value string) { entry.claims = append(entry.claims, lookupKey{kind, owner, value}) }
	link := func(collection, id string) {
		if id != "" {
			entry.groups = append(entry.groups, referenceKey(entityKey{collection, id}))
		}
	}
	var identity, sessionID string
	switch v := object.(type) {
	case projectmodel.Project:
		identity = v.ID.String()
		link("workspace", v.DefaultWorkspaceID.String())
	case workspacemodel.Workspace:
		identity = v.ID.String()
		group("workspaces-project", v.ProjectID.String(), "")
		claim("workspace-path", v.ProjectID.String(), v.Path)
		link("project", v.ProjectID.String())
	case sessionmodel.Session:
		identity, sessionID = v.ID.String(), v.ID.String()
		group("sessions-project", v.ProjectID.String(), "")
		link("workspace", v.WorkspaceID.String())
	case agentmodel.Agent:
		identity, sessionID = v.ID.String(), v.SessionID.String()
		group("agents-session", sessionID, "")
		group("agents-definition", sessionID, v.DefinitionID.String())
		if v.DefinitionID == agentmodel.DefinitionPrimary {
			claim("primary-agent", sessionID, "")
		}
		link("session", sessionID)
		link("task", v.CurrentTaskID.String())
	case taskmodel.Task:
		identity, sessionID = v.ID.String(), v.SessionID.String()
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
	case toolmodel.Execution:
		identity, sessionID = v.ID.String(), v.SessionID.String()
		claim("tool-call", v.TaskID.String(), v.ProviderToolCallID)
		group("tools-task", v.TaskID.String(), "")
		if !v.Status.Terminal() {
			group("unsettled-session", sessionID, "")
		}
		if v.Status == toolmodel.StatusAwaitingApproval {
			group("awaiting-agent", v.AgentID.String(), "")
		}
		link("task", v.TaskID.String())
	case sessionmodel.SessionContextEntry:
		identity, sessionID = v.ID.String(), v.SessionID.String()
		group("contexts-session", sessionID, "")
		link("session", sessionID)
		link("task", v.SourceTaskID.String())
	case agentmodel.AgentControlCommand:
		identity, sessionID = v.ID.String(), scope
		if v.Status == agentmodel.AgentControlPending {
			group("controls-agent", v.AgentID.String(), "")
		}
		link("agent", v.AgentID.String())
		link("task", v.TargetTaskID.String())
	case workflowmodel.WaitCondition:
		identity, sessionID = v.ID.String(), scope
		link("task", v.TaskID.String())
	case messageRecord:
		identity, sessionID = messageKey(v.OwnerID, v.Data.ID), scope
		if v.Data.Sequence != event.Sequence {
			return key, entry, errors.New("message sequence does not match event sequence")
		}
		// 消息不进入全局 latest / groups，正文与位置列表均按 Session 懒加载。
		entry.groups = nil
	case model.ModelUsageRecord:
		identity, sessionID = v.Key(), v.SessionID
		group("usage-session", sessionID, "")
		link("task", v.TaskID)
	case timing.Record:
		identity, sessionID = v.ID, v.SessionID
		group("timings-agent", v.AgentID, "")
		if v.Status == timing.Running {
			group("running-timings", "", "")
		}
		link("task", v.TaskID)
	default:
		return key, entry, fmt.Errorf("unsupported stored object %T", object)
	}
	if identity != event.Key || sessionID != scope {
		return key, entry, errors.New("object identity or session ownership does not match event")
	}
	return key, entry, nil
}

// restoreIndexEntry 在磁盘输入边界严格解码并校验实体，再恢复其查询索引。
func restoreIndexEntry(scope string, event Event) (indexEntry, error) {
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
		return indexEntry{}, errors.New("unknown event type")
	}
	if (scope == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return indexEntry{}, errors.New("invalid event file ownership")
	}
	if event.Key == "" || event.Key != strings.TrimSpace(event.Key) {
		return indexEntry{}, errors.New("invalid object key")
	}
	var target any
	switch event.Collection {
	case "project":
		target = new(projectmodel.Project)
	case "workspace":
		target = new(workspacemodel.Workspace)
	case "session":
		target = new(sessionmodel.Session)
	case "agent":
		target = new(agentmodel.Agent)
	case "task":
		target = new(taskmodel.Task)
	case "tool":
		target = new(toolmodel.Execution)
	case "context":
		target = new(sessionmodel.SessionContextEntry)
	case "control":
		target = new(agentmodel.AgentControlCommand)
	case "wait":
		target = new(workflowmodel.WaitCondition)
	case "message":
		target = new(messageRecord)
	case "usage":
		target = new(model.ModelUsageRecord)
	case "timing":
		target = new(timing.Record)
	}
	if err := decodeJSON(event.Payload, target); err != nil {
		return indexEntry{}, err
	}
	var err error
	switch value := target.(type) {
	case *projectmodel.Project:
		err = validateProject(*value)
	case *workspacemodel.Workspace:
		err = validateWorkspace(*value)
	case *sessionmodel.Session:
		err = validateSession(*value)
	case *agentmodel.Agent:
		err = validateAgent(*value)
	case *taskmodel.Task:
		err = validateTask(*value)
	case *toolmodel.Execution:
		err = validateToolExecution(*value)
	case *sessionmodel.SessionContextEntry:
		err = validateContextEntry(*value)
	case *agentmodel.AgentControlCommand:
		err = validateControlCommand(*value)
	case *workflowmodel.WaitCondition:
		err = validateWaitCondition(*value)
	case *messageRecord:
		err = value.Validate()
	case *model.ModelUsageRecord:
		err = validateUsageRecord(*value)
	case *timing.Record:
		err = value.Validate()
	default:
		return indexEntry{}, fmt.Errorf("unsupported stored object %T", target)
	}
	if err != nil {
		return indexEntry{}, err
	}
	_, entry, err := describe(scope, event, reflect.ValueOf(target).Elem().Interface())
	return entry, err
}

// validateSession 在磁盘输入边界拒绝非法会话，不修改持久化字段。
func validateSession(value sessionmodel.Session) error {
	for _, id := range []string{value.ID.String(), value.ProjectID.String(), value.WorkspaceID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("session", "required reference must be canonical and nonempty")
		}
	}
	if value.State != sessionmodel.SessionActive && value.State != sessionmodel.SessionArchived {
		return fault.InvalidValue("session.state", "unknown session state")
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return fault.InvalidValue("session.timestamps", "timestamps are invalid")
	}
	return nil
}

// validateReasoningState 在消息输入边界检查存储字段，不解释 Provider 的 opaque 内容。
func validateReasoningState(state *appcontext.ReasoningState) error {
	if state == nil {
		return errors.New("reasoning state is required")
	}
	if state.ProviderID == "" || state.ProviderID != strings.TrimSpace(state.ProviderID) {
		return errors.New("reasoning state provider ID is required and must be canonical")
	}
	if state.ModelID == "" || state.ModelID != strings.TrimSpace(state.ModelID) {
		return errors.New("reasoning state model ID is required and must be canonical")
	}
	if state.Protocol != string(modelconfig.APIFormatOpenAIResponses) && state.Protocol != string(modelconfig.APIFormatAnthropicMessages) {
		return errors.New("unsupported reasoning state protocol")
	}
	if len(state.Payload) == 0 || len(state.Payload) > appcontext.MaxReasoningStateBytes {
		return errors.New("invalid or oversized reasoning state payload")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(state.Payload, &object); err != nil || object == nil {
		return errors.New("reasoning state payload must be an object")
	}
	return nil
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
			return agentmodel.ErrAgentExecuting
		}
		return nil
	}
	for key, change := range tx.changes {
		for _, claim := range change.entry.claims {
			if old, ok := s.index.unique[claim]; ok && old != key {
				if next, changed := tx.changes[old]; !changed || slices.Contains(next.entry.claims, claim) {
					return fault.ErrRequestConflict
				}
			}
			for other, next := range tx.changes {
				if other != key && slices.Contains(next.entry.claims, claim) {
					return fault.ErrRequestConflict
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
	taskOwner := func(id identity.TaskID, agent identity.AgentID) error {
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
		if err != nil && !errors.Is(err, fault.ErrNotFound) {
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
		var v toolmodel.Execution
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
		return taskOwner(identity.TaskID(v.TaskID), identity.AgentID(v.AgentID))
	case "timing":
		var v timing.Record
		_ = decodeJSON(data, &v)
		return taskOwner(identity.TaskID(v.TaskID), identity.AgentID(v.AgentID))
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
		if agent.SessionID.String() != scope || appendOwner(agent) != v.OwnerID {
			return errors.New("message agent does not match the stream ownership")
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
			if agent.ID != v.AgentID || appendOwner(agent) != v.OwnerID || v.Data.RequestID != task.RequestID.String() {
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
			return fmt.Errorf("%w: %v", fault.ErrRequestConflict, err)
		}
		return nil
	case "tool":
		var prior, next toolmodel.Execution
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if err := toolmodel.ValidateUpdate(prior, next); err != nil {
			return fmt.Errorf("%w: %v", fault.ErrRequestConflict, err)
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
			return fault.ErrRequestConflict
		}
	case "context", "message":
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
