package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// requireMain 是所有 Agent 过程写入的共同边界，不依赖调用方提供的持久化开关。
func requireMain(records []logRecord, scope string, agentID identity.AgentID) error {
	session, err := getRecord[sessionmodel.Session](records, "session", scope)
	if err != nil {
		return err
	}
	if session.MainAgentID != agentID {
		return errors.New("only the session main agent may persist execution data")
	}
	return nil
}

func validateRelations(state *Storage, records []logRecord, scope, collection string, object any) error {
	executionOwner := func(id string, agentID identity.AgentID) error {
		execution, err := getRecord[agentexecution.ExecutionRecord](records, "agent_execution", id)
		if err != nil {
			return err
		}
		if execution.SessionID.String() != scope || agentID != "" && execution.AgentID != agentID {
			return errorcode.ErrRequestConflict
		}
		return requireMain(records, scope, execution.AgentID)
	}
	switch v := object.(type) {
	case projectmodel.Project:
		projects, err := listRecords[projectmodel.Project](records, "project", nil)
		if err != nil {
			return err
		}
		for _, old := range projects {
			if old.ID != v.ID && (old.DefaultWorkspaceID == v.DefaultWorkspaceID || old.Path == v.Path) {
				return errorcode.ErrRequestConflict
			}
		}
		workspaces, err := listRecords[workspacemodel.Workspace](records, "workspace", nil)
		if err != nil {
			return err
		}
		for _, old := range workspaces {
			if old.ID == v.DefaultWorkspaceID || old.ProjectID == v.ID && old.Path == v.Path {
				return errorcode.ErrRequestConflict
			}
		}
	case workspacemodel.Workspace:
		projects, err := listRecords[projectmodel.Project](records, "project", nil)
		if err != nil {
			return err
		}
		if _, err := getRecord[projectmodel.Project](records, "project", v.ProjectID.String()); err != nil {
			return err
		}
		for _, project := range projects {
			if project.DefaultWorkspaceID == v.ID || project.ID == v.ProjectID && project.Path == v.Path {
				return errorcode.ErrRequestConflict
			}
		}
		workspaces, err := listRecords[workspacemodel.Workspace](records, "workspace", nil)
		if err != nil {
			return err
		}
		for _, old := range workspaces {
			if old.ID != v.ID && old.ProjectID == v.ProjectID && old.Path == v.Path {
				return errorcode.ErrRequestConflict
			}
		}
		sessions, err := listRecords[sessionmodel.Session](records, "session", nil)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if session.WorkspaceID == v.ID && session.ProjectID != v.ProjectID {
				return errorcode.ErrRequestConflict
			}
		}
	case sessionmodel.Session:
		if state.projects[scope] != "" && state.projects[scope] != v.ProjectID.String() {
			return errors.New("session project does not match log directory")
		}
		workspace, err := getRecord[workspacemodel.Workspace](records, "workspace", v.WorkspaceID.String())
		if err != nil {
			return err
		}
		if workspace.ProjectID != v.ProjectID {
			return errorcode.ErrRequestConflict
		}
		sessions, err := listRecords[sessionmodel.Session](records, "session", nil)
		if err != nil {
			return err
		}
		for _, old := range sessions {
			if old.ID != v.ID && old.MainAgentID == v.MainAgentID {
				return errorcode.ErrRequestConflict
			}
		}
	case agentexecution.ExecutionRecord:
		if err := requireMain(records, scope, v.AgentID); err != nil {
			return err
		}
		session, err := getRecord[sessionmodel.Session](records, "session", scope)
		if err != nil {
			return err
		}
		if v.Config.AgentDefinitionID != session.AgentDefinitionID {
			return errorcode.ErrRequestConflict
		}
		input, err := findEntry(records, v.AgentID, v.InputEntryID)
		if err != nil {
			return err
		}
		if input.scope != scope || input.entry.Data.Role != role.User || input.entry.Data.RequestID != v.RequestID.String() || input.entry.Data.SourceTaskID != v.SourceTaskID.String() || input.entry.Data.ExecutionID != v.ID.String() {
			return errorcode.ErrRequestConflict
		}
		values, err := listRecords[agentexecution.ExecutionRecord](records, "agent_execution", nil)
		if err != nil {
			return err
		}
		for _, old := range values {
			if old.ID == v.ID {
				continue
			}
			if old.AgentID == v.AgentID && (old.RequestID == v.RequestID || old.Active() && v.Active()) || v.SourceTaskID != "" && old.SourceTaskID == v.SourceTaskID {
				return errorcode.ErrRequestConflict
			}
		}
	case entryRecord:
		if err := requireMain(records, scope, v.AgentID); err != nil {
			return err
		}
		if v.Data.SourceTaskID != "" && (v.Data.Role != role.User || v.Data.ExecutionID == "") {
			return errors.New("task input requires a user message and execution identity")
		}
		if v.Data.IsSummary() {
			if v.Data.AuthorID != v.AgentID.String() || v.Data.ExecutionID == "" || v.Data.RequestID == "" {
				return errors.New("summary ownership is required")
			}
			seen := make(map[string]bool)
			for _, id := range v.Data.Replaces {
				prior, err := findEntry(records, v.AgentID, id)
				if err != nil || seen[id] || prior.scope != scope || prior.entry.Data.Sequence >= v.Data.Sequence {
					return errors.New("summary source is missing, duplicated or follows summary")
				}
				seen[id] = true
			}
		}
		if v.Data.ExecutionID != "" {
			execution, err := getRecord[agentexecution.ExecutionRecord](records, "agent_execution", v.Data.ExecutionID)
			if errors.Is(err, errorcode.ErrNotFound) && v.Data.Role == role.User && !v.Data.IsSummary() && v.Data.ID == "input:"+v.Data.RequestID {
				return nil
			}
			if err != nil {
				return err
			}
			if execution.SessionID.String() != scope || execution.AgentID != v.AgentID || execution.RequestID.String() != v.Data.RequestID {
				return errorcode.ErrRequestConflict
			}
		}
		return validateToolReceipts(records, scope, v)
	case contextRecord:
		if err := requireMain(records, scope, v.AgentID); err != nil {
			return err
		}
		for _, entry := range v.Entries {
			prior, err := findEntry(records, v.AgentID, entry.ID)
			if err != nil {
				return err
			}
			original := prior.entry.Data
			if prior.scope != scope || entry.Sequence != original.Sequence || entry.ExecutionID != original.ExecutionID || entry.RequestID != original.RequestID || entry.Role != original.Role {
				return errorcode.ErrRequestConflict
			}
		}
	case providerapi.UsageRecord:
		return executionOwner(v.ExecutionID, identity.AgentID(v.AgentID))
	case timing.Record:
		return executionOwner(v.ExecutionID, "")
	case workflow.WaitCondition:
		return executionOwner(v.ExecutionID.String(), v.AgentID)
	default:
		return fmt.Errorf("unsupported collection %q", collection)
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
	case "session":
		fields = []string{"ProjectID", "WorkspaceID", "MainAgentID", "AgentDefinitionID", "CreatedAt"}
	case "context":
		fields = []string{"agent_id"}
	case "agent_execution":
		fields = []string{"ID", "SessionID", "AgentID", "RequestID", "InputEntryID", "SourceTaskID", "Config", "CreatedAt"}
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
		fields = []string{"sessionId", "agentId", "executionId", "turnId"}
	case "agent_execution_timing":
		var prior, next timing.Record
		if err := decodeJSON(before, &prior); err != nil {
			return err
		}
		if err := decodeJSON(after, &next); err != nil {
			return err
		}
		if same(prior, next) {
			return nil
		}
		if prior.Status != timing.Running || next.Status == timing.Running || prior.Operation != next.Operation || prior.ParentID != next.ParentID || !prior.StartedAt.Equal(next.StartedAt) {
			return errorcode.ErrRequestConflict
		}
	case "entry":
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
