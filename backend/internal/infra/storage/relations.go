package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	timingmodel "praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/toolexecution"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
)

// validateRelations 用候选实体检查已提交的引用、领取条件和消息配对。
func (s *Store) validateRelations(key entityKey, entry stateEntry) error {
	data := entry.payload
	scope := entry.scope
	reference := func(collection, id string) error {
		_, err := s.payload(entityKey{collection, id})
		if err != nil {
			return err
		}
		if collection == "workspace" || collection == "project" {
			return nil
		}
		ref, ok := s.entry(entityKey{collection, id})
		if !ok || ref.scope != scope {
			return errors.New("reference ownership mismatch")
		}
		return nil
	}
	taskOwner := func(id identity.AgentExecutionID, agent identity.AgentID) error {
		value, err := getLocked[agentexecution.Execution](s, "agent_execution", id.String())
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
		return nil
	case "workspace":
		var v workspacemodel.Workspace
		_ = decodeJSON(data, &v)
		if err := reference("project", v.ProjectID.String()); err != nil {
			return err
		}
		// 已被会话引用的 Workspace 不能被改到另一个项目。
		for _, dependent := range s.keys(referenceKey(key)) {
			if dependent.collection != "session" {
				continue
			}
			session, err := getLocked[sessionmodel.Session](s, "session", dependent.id)
			if err != nil {
				return err
			}
			if session.ProjectID != v.ProjectID {
				return errorcode.ErrRequestConflict
			}
		}
	case "session":
		var v sessionmodel.Session
		_ = decodeJSON(data, &v)
		if !jsonl.ValidScope(v.ProjectID.String()) {
			return errors.New("session project ID is not a valid directory name")
		}
		if projectID := s.log.Project(scope); projectID != "" && projectID != v.ProjectID.String() {
			return errors.New("session project does not match log directory")
		}
		workspace, err := getLocked[workspacemodel.Workspace](s, "workspace", v.WorkspaceID.String())
		if err != nil {
			return err
		}
		if workspace.ProjectID != v.ProjectID {
			return errors.New("session workspace ownership mismatch")
		}
		return s.validateAgentSession(scope, v.MainAgentID)
	case "agent_execution":
		var v agentexecution.Execution
		_ = decodeJSON(data, &v)
		if err := s.validateAgentSession(scope, v.AgentID); err != nil {
			return err
		}
		session, err := getLocked[sessionmodel.Session](s, "session", scope)
		if err != nil {
			return err
		}
		if v.AgentID == session.MainAgentID && v.Config.AgentDefinitionID != session.AgentDefinitionID {
			return errors.New("execution does not match the main agent definition")
		}
		input, err := s.payload(entryEntityKey(scope, v.AgentID, v.InputEntryID))
		if err != nil {
			return err
		}
		var record entryRecord
		if err := decodeJSON(input, &record); err != nil {
			return err
		}
		if record.AgentID != v.AgentID || record.Data.Role != role.User || record.Data.RequestID != v.RequestID.String() || record.Data.SourceTaskID != v.SourceTaskID.String() || record.Data.ExecutionID != v.ID.String() {
			return errorcode.ErrRequestConflict
		}
	case "tool_event":
		var v toolexecution.Event
		_ = decodeJSON(data, &v)
		execution, err := getLocked[agentexecution.Execution](s, "agent_execution", v.ExecutionID.String())
		if err != nil {
			return err
		}
		if !execution.Active() {
			return errorcode.ErrRequestConflict
		}
		call, err := (ToolCallQueries{s}).getCallLocked(v.Key)
		if err != nil {
			return err
		}
		if call.SessionID.String() != scope || v.At.Before(call.CreatedAt) {
			return errorcode.ErrRequestConflict
		}

		if err := (ToolExecutionRepository{s}).validateAuthorizationLocked(v); err != nil {
			return err
		}
		if _, err := s.lookup(lookupKey{kind: "tool-result", owner: v.ExecutionID.String(), value: v.CallID}); err == nil {
			return errorcode.ErrRequestConflict
		} else if !errors.Is(err, errorcode.ErrNotFound) {
			return err
		}
	case "wait":
		var v workflow.WaitCondition
		_ = decodeJSON(data, &v)
		return taskOwner(v.ExecutionID, v.AgentID)
	case "usage":
		var v providerapi.UsageRecord
		_ = decodeJSON(data, &v)
		return taskOwner(identity.AgentExecutionID(v.ExecutionID), identity.AgentID(v.AgentID))
	case "agent_execution_timing":
		var v timingmodel.Record
		_ = decodeJSON(data, &v)
		return reference("agent_execution", v.ExecutionID)
	case "entry":
		var v entryRecord
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		if err := s.validateAgentSession(scope, v.AgentID); err != nil {
			return err
		}
		if v.Data.IsSummary() {
			seen := make(map[string]bool, len(v.Data.Replaces))
			for _, id := range v.Data.Replaces {
				if seen[id] {
					return errors.New("summary repeats a source entry")
				}
				seen[id] = true
				prior, exists := s.entry(entryEntityKey(scope, v.AgentID, id))
				if !exists || prior.eventSequence >= entry.eventSequence {
					return errors.New("summary source entry is missing or follows the summary")
				}
			}
		}
		if v.Data.SourceTaskID != "" && (v.Data.Role != role.User || v.Data.ExecutionID == "") {
			return errors.New("task input requires a user message and execution identity")
		}
		if v.Data.ExecutionID != "" {
			task, err := getLocked[agentexecution.Execution](s, "agent_execution", v.Data.ExecutionID)
			if errors.Is(err, errorcode.ErrNotFound) && v.Data.Role == role.User && v.Data.ID == "input:"+v.Data.RequestID {
				return nil
			}
			if err != nil {
				return err
			}
			if task.AgentID != v.AgentID || task.SessionID.String() != scope || v.Data.RequestID != task.RequestID.String() {
				return errors.New("message stream or request ownership mismatch")
			}
			if v.Data.Role == role.Assistant && (v.Data.AuthorID != task.AgentID.String() || !task.Active() || task.Progress.FinalAssistantID != "") {
				return errors.New("assistant author or execution state mismatch")
			}
		}
		return s.validateToolReceipts(entry)
	}
	return nil
}

// validateAgentSession 仅校验持久化引用的一致性，不保存或查询 Agent 对象。
// 首条消息可先于 Execution 提交，但同一 AgentID 不能出现在两个 Session 的事实中。
func (s *Store) validateAgentSession(scope string, id identity.AgentID) error {
	for key := range s.state.groups[lookupKey{kind: "agent-records", owner: id.String()}] {
		if s.state.entries[key].scope != scope {
			return errorcode.ErrRequestConflict
		}
	}
	return nil
}

func immutable(collection string, before, after json.RawMessage) error {
	// Execution 由所属执行者串行修改，不重复比较内部生成的执行快照。
	if collection == "agent_execution" {
		return nil
	}
	var left, right map[string]json.RawMessage
	if err := json.Unmarshal(before, &left); err != nil {
		return err
	}
	if err := json.Unmarshal(after, &right); err != nil {
		return err
	}
	var fields []string
	switch collection {
	case "project":
		fields = []string{"ID", "Name", "Path", "DefaultWorkspaceID", "CreatedAt"}
	case "session":
		fields = []string{
			"ProjectID",
			"WorkspaceID",
			"MainAgentID",
			"AgentDefinitionID",
			"CreatedAt",
		}
	case "usage":
		var prior, next providerapi.UsageRecord
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if next.Usage.InputTokens < prior.Usage.InputTokens {
			return errorcode.ErrRequestConflict
		}
		for _, counts := range [][2]*int64{
			{prior.Usage.OutputTokens, next.Usage.OutputTokens},
			{prior.Usage.CacheReadInputTokens, next.Usage.CacheReadInputTokens},
			{prior.Usage.CacheCreationInputTokens, next.Usage.CacheCreationInputTokens},
		} {
			if counts[0] != nil && (counts[1] == nil || *counts[1] < *counts[0]) {
				return errorcode.ErrRequestConflict
			}
		}
		fields = []string{"agentId", "executionId", "turnId"}
	case "agent_execution_timing":
		var prior, next timingmodel.Record
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if same(prior, next) {
			return nil
		}
		if prior.Status != timingmodel.Running || next.Status == timingmodel.Running || prior.Operation != next.Operation || prior.ParentID != next.ParentID || !prior.StartedAt.Equal(next.StartedAt) {
			return errorcode.ErrRequestConflict
		}

	case "entry", "tool_event":
		if !bytes.Equal(before, after) {
			return errorcode.ErrRequestConflict
		}
	}
	for _, field := range fields {
		if !bytes.Equal(left[field], right[field]) {
			return fmt.Errorf("%w: %s.%s is immutable", errorcode.ErrRequestConflict, collection, field)
		}
	}
	return nil
}
