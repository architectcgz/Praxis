package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/session/entry/role"
	timingmodel "praxis/internal/core/timing"
	"praxis/internal/core/toolexecution"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/workflow"
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
		value, err := getLocked[agentmodel.Execution](s, "agent_execution", id.String())
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
				return fault.ErrRequestConflict
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
	case "agent":
		return reference("session", scope)
	case "agent_execution":
		var v agentmodel.Execution
		_ = decodeJSON(data, &v)
		agent, err := getLocked[agentmodel.Agent](s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if agent.SessionID != v.SessionID {
			return errors.New("execution does not match agent ownership")
		}
		owner := ""
		if !agent.CanReadSessionContext() {
			owner = agent.ID.String()
		}
		input, err := s.payload(entryEntityKey(scope, owner, v.InputEntryID))
		if err != nil {
			return err
		}
		var record entryRecord
		if err := decodeJSON(input, &record); err != nil {
			return err
		}
		if record.AgentID != v.AgentID || record.Data.Role != role.User || record.Data.RequestID != v.RequestID.String() || record.Data.SourceTaskID != v.SourceTaskID.String() || record.Data.ExecutionID != v.ID.String() {
			return fault.ErrRequestConflict
		}
	case "tool_event":
		var v toolexecution.Event
		_ = decodeJSON(data, &v)
		execution, err := getLocked[agentmodel.Execution](s, "agent_execution", v.ExecutionID.String())
		if err != nil {
			return err
		}
		if !execution.Active() {
			return fault.ErrRequestConflict
		}
		call, err := (ToolCallQueries{s}).getCallLocked(v.Key)
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
		if _, err := s.lookup(lookupKey{kind: "tool-result", owner: v.ExecutionID.String(), value: v.CallID}); err == nil {
			return fault.ErrRequestConflict
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		facts, err := (ToolExecutionRepository{s}).readLocked(v.Key)
		if err != nil {
			return err
		}
		return facts.Apply(v)
	case "context":
		var v sessionmodel.SharedFact
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
		var v providerapi.UsageRecord
		_ = decodeJSON(data, &v)
		return taskOwner(identity.AgentExecutionID(v.ExecutionID), identity.AgentID(v.AgentID))
	case "timing":
		var v timingmodel.Record
		_ = decodeJSON(data, &v)
		return taskOwner(identity.AgentExecutionID(v.ExecutionID), identity.AgentID(v.AgentID))
	case "entry":
		var v entryRecord
		_ = decodeJSON(data, &v)
		if err := reference("session", scope); err != nil {
			return err
		}
		agent, err := getLocked[agentmodel.Agent](s, "agent", v.AgentID.String())
		if err != nil {
			return err
		}
		if agent.SessionID.String() != scope || v.OwnerID != "" && v.OwnerID != v.AgentID.String() {
			return errors.New("message agent does not match the stream ownership")
		}
		if v.Data.IsSummary() {
			seen := make(map[string]bool, len(v.Data.Replaces))
			for _, id := range v.Data.Replaces {
				if seen[id] {
					return errors.New("summary repeats a source entry")
				}
				seen[id] = true
				prior, exists := s.entry(entryEntityKey(scope, v.OwnerID, id))
				if !exists || prior.eventSequence >= entry.eventSequence {
					return errors.New("summary source entry is missing or follows the summary")
				}
			}
		}
		if v.Data.SourceTaskID != "" && (v.Data.Role != role.User || v.Data.ExecutionID == "") {
			return errors.New("task input requires a user message and execution identity")
		}
		if v.Data.ExecutionID != "" {
			task, err := getLocked[agentmodel.Execution](s, "agent_execution", v.Data.ExecutionID)
			if errors.Is(err, fault.ErrNotFound) && v.Data.Role == role.User && v.Data.ID == "input:"+v.Data.RequestID {
				return nil
			}
			if err != nil {
				return err
			}
			agent, err := getLocked[agentmodel.Agent](s, "agent", task.AgentID.String())
			if err != nil {
				return err
			}
			if agent.ID != v.AgentID || task.SessionID.String() != scope || v.Data.RequestID != task.RequestID.String() {
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
	case "project":
		fields = []string{"ID", "Name", "Path", "DefaultWorkspaceID", "CreatedAt"}
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
	case "agent":
		fields = []string{
			"ID",
			"SessionID",
			"DefinitionID",
			"Profile",
			"CreatedAt",
		}
	case "session":
		fields = []string{"ProjectID", "WorkspaceID", "CreatedAt"}
	case "usage":
		var prior, next providerapi.UsageRecord
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
	case "context", "entry", "tool_event", "control":
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
