package storage

import (
	"bytes"
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
	"praxis/internal/infra/jsonl"
	"praxis/internal/workflow"
	"reflect"
	"strings"
)

// describe 直接从实体构建查询索引，不重新解析或校验业务输入。
func describe(scope string, event jsonl.Event, object any) (entityKey, stateEntry, error) {
	key := eventKey(scope, event)
	entry := stateEntry{
		scope:         scope,
		payload:       bytes.Clone(event.Payload),
		groups:        []lookupKey{collectionKey(event.Collection), scopeKey(scope)},
		eventSequence: event.Sequence,
	}
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
		link("agent_execution", v.CurrentExecutionID.String())
	case agentmodel.Execution:
		identity, sessionID = v.ID.String(), v.SessionID.String()

		group("executions-agent", v.AgentID.String(), "")
		claim("execution-request", v.AgentID.String(), v.RequestID.String())
		if v.Active() {
			claim("active-agent", v.AgentID.String(), "")
			group("active-session", sessionID, "")
			group("active-executions", "", "")
		}
		link("agent", v.AgentID.String())
	case queued.Task:
		identity, sessionID = v.ID.String(), v.SessionID.String()
		entry.orderSequence = v.Sequence
		group("tasks-agent", v.AgentID.String(), "")
		claim("task-request", v.AgentID.String(), v.RequestID.String())
		claim("task-sequence", v.AgentID.String(), fmt.Sprint(v.Sequence))
		if v.Status == queued.Pending {
			group("pending-agent", v.AgentID.String(), "")
		}
		link("agent", v.AgentID.String())
		link("agent_execution", v.ExecutionID.String())
	case toolexecution.Event:
		if event.Type != string(v.Kind) {
			return key, entry, errors.New("tool event payload does not match its event type")
		}
		identity, sessionID = fmt.Sprintf("%s@%d", v.Key, event.Sequence), v.SessionID.String()
		group("tool-events", v.ExecutionID.String(), v.CallID)
		link("agent_execution", v.ExecutionID.String())
	case sessionmodel.SessionContextEntry:
		identity, sessionID = v.ID.String(), v.SessionID.String()
		group("contexts-session", sessionID, "")
		link("session", sessionID)
		link("agent_execution", v.SourceExecutionID.String())
	case agentmodel.AgentControlCommand:
		identity, sessionID = v.ID.String(), scope
		if v.Status == agentmodel.AgentControlPending {
			group("controls-agent", v.AgentID.String(), "")
		}
		link("agent", v.AgentID.String())
		link("agent_execution", v.TargetExecutionID.String())
	case workflow.WaitCondition:
		identity, sessionID = v.ID.String(), scope
		link("agent_execution", v.ExecutionID.String())
	case messageRecord:
		identity, sessionID = messageKey(v.OwnerID, v.Data.ID), scope
		if v.Data.Sequence != event.Sequence {
			return key, entry, errors.New("message sequence does not match event sequence")
		}
		group("messages-session", scope, "")
		group("messages-stream", scope, v.OwnerID)
		link("session", scope)
		link("agent", v.AgentID.String())
		link("agent_execution", v.Data.ExecutionID)
		hasCalls := false
		for _, block := range v.Data.Blocks {
			if block.Kind != messagemodel.BlockToolCall && block.Kind != messagemodel.BlockToolResult {
				continue
			}
			if v.Data.ExecutionID == "" || v.Data.TurnID == "" || block.CallID == "" {
				return key, entry, errors.New("tool message ownership is required")
			}
			if block.Kind == messagemodel.BlockToolCall {
				claim("tool-call", v.Data.ExecutionID, block.CallID)
				hasCalls = true
			} else {
				claim("tool-result", v.Data.ExecutionID, block.CallID)
			}
		}
		if hasCalls {
			group("tool-call-messages", v.Data.ExecutionID, "")
			group("all-tool-call-messages", "", "")
		}
	case modelapi.ModelUsageRecord:
		identity, sessionID = v.Key(), v.SessionID
		group("usage-session", sessionID, "")
		link("agent_execution", v.ExecutionID)
	case timingmodel.Record:
		identity, sessionID = v.ID, v.SessionID
		group("timings-agent", v.AgentID, "")
		if v.Status == timingmodel.Running {
			group("running-timings", "", "")
		}
		link("agent_execution", v.ExecutionID)
	default:
		return key, entry, fmt.Errorf("unsupported stored object %T", object)
	}
	if identity != event.Key || sessionID != scope {
		return key, entry, errors.New("object identity or session ownership does not match event")
	}
	return key, entry, nil
}

// restoreStateEntry 在磁盘输入边界严格解码并校验实体，再恢复其查询索引。
func restoreStateEntry(scope string, event jsonl.Event) (stateEntry, error) {
	kinds := map[string]string{
		"project":         "project.saved",
		"workspace":       "workspace.saved",
		"session":         "session.saved",
		"agent":           "agent.saved",
		"agent_execution": "agent_execution.saved",
		"task":            "task.saved",
		"wait":            "wait.saved",
		"control":         "control.saved",
		"context":         "context.entry_appended",
		"message":         "message.appended",
		"timing":          "timing.saved",
		"usage":           "usage.saved",
	}
	if event.Collection == "tool_event" {
		kinds["tool_event"] = string(toolexecution.EventKind(event.Type))
		if !validToolEventKind(toolexecution.EventKind(event.Type)) {
			return stateEntry{}, errors.New("unknown tool control event")
		}
	}
	if kinds[event.Collection] == "" || kinds[event.Collection] != event.Type {
		return stateEntry{}, errors.New("unknown event type")
	}
	if (scope == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return stateEntry{}, errors.New("invalid event file ownership")
	}
	if event.Key == "" || event.Key != strings.TrimSpace(event.Key) {
		return stateEntry{}, errors.New("invalid object key")
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
		target = new(queued.Task)
	case "agent_execution":
		target = new(agentmodel.Execution)
	case "tool_event":
		target = new(toolexecution.Event)
	case "context":
		target = new(sessionmodel.SessionContextEntry)
	case "control":
		target = new(agentmodel.AgentControlCommand)
	case "wait":
		target = new(workflow.WaitCondition)
	case "message":
		target = new(messageRecord)
	case "usage":
		target = new(modelapi.ModelUsageRecord)
	case "timing":
		target = new(timingmodel.Record)
	}
	if err := decodeJSON(event.Payload, target); err != nil {
		return stateEntry{}, err
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
	case *agentmodel.Execution:
		err = validateAgentExecution(*value)
	case *queued.Task:
		err = validateQueuedTask(*value)
	case *toolexecution.Event:
		err = validateToolEvent(*value)
	case *sessionmodel.SessionContextEntry:
		err = validateContextEntry(*value)
	case *agentmodel.AgentControlCommand:
		err = validateControlCommand(*value)
	case *workflow.WaitCondition:
		err = validateWaitCondition(*value)
	case *messageRecord:
		err = value.Validate()
	case *modelapi.ModelUsageRecord:
		err = validateUsageRecord(*value)
	case *timingmodel.Record:
		err = value.Validate()
	default:
		return stateEntry{}, fmt.Errorf("unsupported stored object %T", target)
	}
	if err != nil {
		return stateEntry{}, err
	}
	_, entry, err := describe(scope, event, reflect.ValueOf(target).Elem().Interface())
	return entry, err
}
