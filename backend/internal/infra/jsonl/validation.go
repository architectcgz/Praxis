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
		return key, entry, errors.New("未知事件类型")
	}
	if (scope == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return key, entry, errors.New("事件文件归属非法")
	}
	if event.Key == "" || event.Key != strings.TrimSpace(event.Key) {
		return key, entry, errors.New("对象键非法")
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
			return key, entry, errors.New("消息序号与事件序号不一致")
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
		return key, entry, errors.New("对象身份或 Session 归属与事件不一致")
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
		"FailureMessage",
	} {
		v := value.FieldByName(field)
		if v.IsValid() && v.Kind() == reflect.String && v.String() != strings.TrimSpace(v.String()) {
			return key, entry, fmt.Errorf("%s 必须已规范化", field)
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
			return errors.New("引用归属不符")
		}
		return nil
	}
	taskOwner := func(id contracts.TaskID, agent contracts.AgentID) error {
		value, err := get[taskmodel.Task](ctx, s, "task", id.String())
		if err != nil {
			return err
		}
		if value.SessionID.String() != scope || value.AgentID != agent {
			return errors.New("Task 与 Agent 归属不符")
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
			return errors.New("Project 默认工作区归属不符")
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
			return errors.New("Session 工作区归属不符")
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
			return errors.New("Agent 与活动 Task 不一致")
		}
	case "task":
		var v taskmodel.Task
		_ = decodeJSON(data, &v)
		agent, err := get[agentmodel.Agent](ctx, s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if agent.SessionID != v.SessionID || v.Active() != (agent.CurrentTaskID == v.ID) {
			return errors.New("Task 与 Agent 活动身份不一致")
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
				return errors.New("消息流或请求归属不符")
			}
			if v.Data.Role == sessionmodel.RoleAssistant && v.Data.AuthorID != task.AgentID.String() {
				return errors.New("Assistant 作者归属不符")
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
		if err := json.Unmarshal(before, &prior); err != nil {
			return err
		}
		if err := json.Unmarshal(after, &next); err != nil {
			return err
		}
		allowed := prior.Status == next.Status
		switch prior.Status {
		case taskmodel.TaskPending:
			allowed = allowed || next.Status == taskmodel.TaskStarting
		case taskmodel.TaskStarting:
			allowed = allowed || next.Status == taskmodel.TaskRunning || next.Status == taskmodel.TaskEnded
		case taskmodel.TaskRunning:
			allowed = allowed || next.Status == taskmodel.TaskEnding || next.Status == taskmodel.TaskEnded
		case taskmodel.TaskEnding:
			allowed = allowed || next.Status == taskmodel.TaskEnded
		}
		if !allowed {
			return contracts.ErrRequestConflict
		}
		if next.Progress.NextSequence < prior.Progress.NextSequence || next.Progress.ModelRequests < prior.Progress.ModelRequests || next.Progress.OutputBytes < prior.Progress.OutputBytes || next.Progress.ToolCalls < prior.Progress.ToolCalls {
			return contracts.ErrRequestConflict
		}
		if prior.Progress.FinalAssistantID != "" && (prior.Progress.FinalAssistantID != next.Progress.FinalAssistantID || prior.Progress.FinalStopReason != next.Progress.FinalStopReason) {
			return contracts.ErrRequestConflict
		}
		fields = []string{
			"ID",
			"AgentID",
			"RequestID",
			"Sequence",
			"ProviderID",
			"ModelID",
			"ReasoningLevel",
			"CreatedAt",
			"InputMessageID",
		}
		if string(left["Status"]) != `"pending"` {
			fields = append(fields, "Config", "ContextSelection")
		} else if string(right["Status"]) != `"pending"` && string(right["Status"]) != `"starting"` {
			return contracts.ErrRequestConflict
		}
	case "tool":
		var prior, next toolmodel.ToolInvocation
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		allowed := prior.Status == next.Status
		switch prior.Status {
		case toolmodel.ToolInvocationRequested:
			allowed = allowed || next.Status == toolmodel.ToolInvocationAwaitingApproval || next.Status == toolmodel.ToolInvocationApproved || next.Status == toolmodel.ToolInvocationDenied || next.Status == toolmodel.ToolInvocationInterrupted
		case toolmodel.ToolInvocationAwaitingApproval:
			allowed = allowed || next.Status == toolmodel.ToolInvocationApproved || next.Status == toolmodel.ToolInvocationDenied || next.Status == toolmodel.ToolInvocationInterrupted
		case toolmodel.ToolInvocationApproved:
			allowed = allowed || next.Status == toolmodel.ToolInvocationRunning || next.Status == toolmodel.ToolInvocationInterrupted
		case toolmodel.ToolInvocationRunning:
			allowed = allowed || next.Status == toolmodel.ToolInvocationSucceeded || next.Status == toolmodel.ToolInvocationFailed || next.Status == toolmodel.ToolInvocationInterrupted || next.Status == toolmodel.ToolInvocationUnknown
		}
		if !allowed {
			return contracts.ErrRequestConflict
		}
		if string(left["BudgetReserved"]) == "true" && string(right["BudgetReserved"]) != "true" {
			return contracts.ErrRequestConflict
		}
		fields = []string{
			"ID",
			"TaskID",
			"TurnID",
			"AgentID",
			"ProviderToolCallID",
			"Name",
			"Arguments",
			"ArgumentsDigest",
			"CreatedAt",
		}
		// 原始调用先落库，规范化参数只允许随 requested -> approved 填充一次。
		if (!bytes.Equal(left["NormalizedArguments"], right["NormalizedArguments"]) || !bytes.Equal(left["Path"], right["Path"])) &&
			(string(left["Status"]) != `"requested"` || (string(right["Status"]) != `"approved"` && string(right["Status"]) != `"awaiting_approval"` && string(right["Status"]) != `"denied"`) ||
				len(left["NormalizedArguments"]) > 0 && string(left["NormalizedArguments"]) != "null") {
			return contracts.ErrRequestConflict
		}
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
			return fmt.Errorf("%w: %s.%s 不可变", contracts.ErrRequestConflict, collection, field)
		}
	}
	if collection == "task" && string(left["Status"]) == `"ended"` && !bytes.Equal(before, after) {
		return contracts.ErrRequestConflict
	}
	if collection == "tool" {
		var status toolmodel.ToolInvocationStatus
		if err := json.Unmarshal(left["Status"], &status); err != nil {
			return err
		}
		if status.Terminal() && !bytes.Equal(before, after) {
			return contracts.ErrRequestConflict
		}
	}
	return nil
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		limit = 100
	}
	return values[:min(len(values), limit)]
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }
func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
