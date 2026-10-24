package storage

import (
	"bytes"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	timingmodel "praxis/internal/core/agent/execution/timing"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/toolexecution"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/errorcode"
	"praxis/internal/infra/jsonl"
	"reflect"
	"strings"
	"time"
)

// describe 直接从实体构建查询索引，不重新解析或校验业务输入。
func (s *Store) describe(scope string, event jsonl.Event, object any) (entityKey, stateEntry, error) {
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
	var entityID, sessionID string
	switch v := object.(type) {
	case projectmodel.Project:
		entityID = v.ID.String()
		claim("workspace-id", v.DefaultWorkspaceID.String(), "")
		claim("workspace-path", v.ID.String(), v.Path)
	case workspacemodel.Workspace:
		entityID = v.ID.String()
		claim("workspace-id", v.ID.String(), "")
		group("workspaces-project", v.ProjectID.String(), "")
		claim("workspace-path", v.ProjectID.String(), v.Path)
		link("project", v.ProjectID.String())
	case sessionmodel.Session:
		entityID, sessionID = v.ID.String(), v.ID.String()
		group("sessions-project", v.ProjectID.String(), "")
		link("workspace", v.WorkspaceID.String())
	case agentmodel.Agent:
		entityID, sessionID = v.ID.String(), v.SessionID.String()
		group("agents-session", sessionID, "")
		if v.IsMain() {
			group("agents-main", sessionID, "")
			claim("main-agent", sessionID, "")
		}
		link("session", sessionID)
	case agentexecution.Execution:
		entityID, sessionID = v.ID.String(), v.SessionID.String()

		group("executions-agent", v.AgentID.String(), "")
		claim("execution-request", v.AgentID.String(), v.RequestID.String())
		if v.SourceTaskID != "" {
			claim("execution-task", v.SourceTaskID.String(), "")
		}
		if v.Active() {
			claim("active-agent", v.AgentID.String(), "")
			claim("active-session", sessionID, "")
			group("active-session", sessionID, "")
			group("active-executions", "", "")
		}
		link("agent", v.AgentID.String())
	case toolexecution.Event:
		if event.Type != string(v.Kind) {
			return key, entry, errors.New("tool event payload does not match its event type")
		}
		entityID, sessionID = fmt.Sprintf("%s@%d", v.Key, event.Sequence), v.SessionID.String()
		group("tool-event-kind", v.Key.String(), string(v.Kind))
		link("agent_execution", v.ExecutionID.String())
	case agentmodel.AgentControlCommand:
		entityID, sessionID = v.ID.String(), scope
		group("controls-agent", v.AgentID.String(), "")
		link("agent", v.AgentID.String())
		link("agent_execution", v.TargetExecutionID.String())
	case workflow.WaitCondition:
		entityID, sessionID = v.ID.String(), scope
		link("agent_execution", v.ExecutionID.String())
	case entryRecord:
		entry.record = &sessionentry.Record{AgentID: v.AgentID, Data: v.Data}
		entityID, sessionID = entryKey(v.AgentID, v.Data.ID), scope
		if v.Data.Sequence != event.Sequence {
			return key, entry, errors.New("message sequence does not match event sequence")
		}
		group("entries-session", scope, "")
		if v.Data.ExecutionID != "" {
			group("entries-execution", v.Data.ExecutionID, "")
		}
		link("session", scope)
		link("agent", v.AgentID.String())
		link("agent_execution", v.Data.ExecutionID)
		hasCalls := false
		for _, block := range v.Data.Content {
			if block.Kind != sessionentry.BlockToolCall && block.Kind != sessionentry.BlockToolResult {
				continue
			}
			if v.Data.ExecutionID == "" || v.Data.TurnID == "" || block.CallID == "" {
				return key, entry, errors.New("tool message ownership is required")
			}
			if block.Kind == sessionentry.BlockToolCall {
				claim("tool-call", v.Data.ExecutionID, block.CallID)
				hasCalls = true
			} else {
				claim("tool-result", v.Data.ExecutionID, block.CallID)
			}
		}
		if hasCalls {
			group("tool-call-entries", v.Data.ExecutionID, "")
			group("all-tool-call-entries", "", "")
		}
	case providerapi.UsageRecord:
		entityID, sessionID = v.Key(), v.SessionID
		group("usage-session", sessionID, "")
		link("agent_execution", v.ExecutionID)
	case timingmodel.Record:
		if (event.Type == "agent_execution.timing_started") != (v.Status == timingmodel.Running) || v.Status == timingmodel.Interrupted {
			return key, entry, errors.New("timing state does not match its event type")
		}
		data, err := s.payload(entityKey{"agent_execution", v.ExecutionID})
		if err != nil {
			return key, entry, err
		}
		var execution agentexecution.Execution
		if err := decodeJSON(data, &execution); err != nil {
			return key, entry, err
		}
		entityID, sessionID = timingKey(v.ExecutionID, v.ID), execution.SessionID.String()
		group("timings-agent", execution.AgentID.String(), "")
		group("timings-execution", v.ExecutionID, "")
		if v.Status == timingmodel.Running {
			group("running-timings", "", "")
		}
		link("agent_execution", v.ExecutionID)
	default:
		return key, entry, fmt.Errorf("unsupported stored object %T", object)
	}
	if entityID != event.Key || sessionID != scope {
		return key, entry, errors.New("object identity or session ownership does not match event")
	}
	return key, entry, nil
}

// restoreStateEntry 在磁盘输入边界严格解码并校验实体，再恢复其查询索引。
func (s *Store) restoreStateEntry(scope string, event jsonl.Event) (stateEntry, error) {
	if event.Type == "timing.saved" {
		return stateEntry{}, errors.New("legacy timing.saved events are unsupported; archive the existing state directory and start with a fresh state directory")
	}
	kinds := map[string]string{
		"project":         "project.saved",
		"workspace":       "workspace.saved",
		"session":         "session.saved",
		"agent":           "agent.saved",
		"agent_execution": "agent_execution.saved",
		"wait":            "wait.saved",
		"control":         "control.saved",
		"entry":           "entry.appended",
		"usage":           "usage.saved",
	}
	if event.Collection == "agent_execution_timing" && (event.Type == "agent_execution.timing_started" || event.Type == "agent_execution.timing_finished") {
		kinds[event.Collection] = event.Type
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
	case "agent_execution":
		target = new(agentexecution.Execution)
	case "tool_event":
		target = new(toolexecution.Event)
	case "control":
		target = new(agentmodel.AgentControlCommand)
	case "wait":
		target = new(workflow.WaitCondition)
	case "entry":
		target = new(entryRecord)
	case "usage":
		target = new(providerapi.UsageRecord)
	case "agent_execution_timing":
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
	case *agentexecution.Execution:
		err = validateAgentExecution(*value)
	case *toolexecution.Event:
		err = validateToolEvent(*value)
	case *agentmodel.AgentControlCommand:
		err = validateControlCommand(*value)
	case *workflow.WaitCondition:
		err = validateWaitCondition(*value)
	case *entryRecord:
		err = value.Validate()
	case *providerapi.UsageRecord:
		err = validateUsageRecord(*value)
	case *timingmodel.Record:
		err = value.Validate()
	default:
		return stateEntry{}, fmt.Errorf("unsupported stored object %T", target)
	}
	if err != nil {
		return stateEntry{}, err
	}
	_, entry, err := s.describe(scope, event, reflect.ValueOf(target).Elem().Interface())
	return entry, err
}

// decodeProjected 在同一次持锁读取中组合权威事实，返回值不与 State 共享可变数据。
func (s *Store) decodeProjected(data []byte, target any) error {
	if err := decodeJSON(data, target); err != nil {
		return err
	}
	switch value := target.(type) {
	case *timingmodel.Record:
		entry, _ := s.entry(entityKey{"agent_execution_timing", timingKey(value.ExecutionID, value.ID)})
		if entry.interrupted {
			value.Status = timingmodel.Interrupted
		}
	case *agentmodel.Agent:
		return s.projectAgent(value)
	case *agentexecution.Execution:
		return s.projectExecution(value)
	case *agentmodel.AgentControlCommand:
		value.Status, value.AppliedAt = agentmodel.AgentControlPending, time.Time{}
		execution, err := getLocked[agentexecution.Execution](s, "agent_execution", value.TargetExecutionID.String())
		if err != nil {
			return err
		}
		if !execution.Active() {
			at := execution.EndedAt
			if value.CreatedAt.After(at) {
				at = value.CreatedAt
			}
			value.MarkApplied(at)
		}
	}
	return nil
}

func (s *Store) projectAgent(value *agentmodel.Agent) error {
	value.State, value.CurrentExecutionID = agentmodel.AgentIdle, ""
	key, err := s.lookup(lookupKey{kind: "active-agent", owner: value.ID.String()})
	if err != nil && !errors.Is(err, errorcode.ErrNotFound) {
		return err
	}
	var latest agentexecution.Execution
	keys := []entityKey{key}
	if errors.Is(err, errorcode.ErrNotFound) {
		keys = s.keys(lookupKey{kind: "executions-agent", owner: value.ID.String()})
	}
	// ponytail: 无活动执行时扫描该 Agent 的历史；历史查询成为瓶颈时再增加最新执行索引。
	for _, key := range keys {
		data, err := s.payload(key)
		if err != nil {
			return err
		}
		var execution agentexecution.Execution
		if err := decodeJSON(data, &execution); err != nil {
			return err
		}
		if latest.ID == "" || execution.CreatedAt.After(latest.CreatedAt) || execution.CreatedAt.Equal(latest.CreatedAt) && execution.ID > latest.ID {
			latest = execution
		}
	}
	if latest.ID == "" {
		return nil
	}
	for _, at := range []time.Time{latest.CreatedAt, latest.StartedAt, latest.EndedAt} {
		if at.After(value.UpdatedAt) {
			value.UpdatedAt = at
		}
	}
	if latest.Active() {
		value.State, value.CurrentExecutionID = agentmodel.AgentExecuting, latest.ID
		for _, key := range s.keys(lookupKey{kind: "controls-agent", owner: value.ID.String()}) {
			data, err := s.payload(key)
			if err != nil {
				return err
			}
			var control agentmodel.AgentControlCommand
			if err := decodeJSON(data, &control); err != nil {
				return err
			}
			if control.TargetExecutionID == latest.ID {
				value.State = agentmodel.AgentPausing
				if control.CreatedAt.After(value.UpdatedAt) {
					value.UpdatedAt = control.CreatedAt
				}
			}
		}
		return nil
	}
	switch latest.Outcome {
	case agentexecution.ExecutionYielded:
		value.State = agentmodel.AgentWaiting
	case agentexecution.ExecutionPaused:
		value.State = agentmodel.AgentPaused
	case agentexecution.ExecutionFailed:
		value.State = agentmodel.AgentFailed
	case agentexecution.ExecutionInterrupted:
		value.State = agentmodel.AgentInterrupted
	}
	return nil
}

func (s *Store) projectExecution(value *agentexecution.Execution) error {
	value.Progress.OutputBytes = value.Progress.ModelOutputBytes
	value.Progress.ToolCalls = 0
	value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
	var lastAssistant sessionentry.ContextEntry
	// ponytail: 按执行扫描消息派生统计；单次执行消息量成为瓶颈时再缓存增量计数。
	for _, key := range s.keys(lookupKey{kind: "entries-execution", owner: value.ID.String()}) {
		source, _ := s.entry(key)
		record := source.entryValue()
		message := record.Data
		if message.Role == role.Assistant && message.Sequence > lastAssistant.Sequence {
			lastAssistant = message
		}
		for _, block := range message.Content {
			switch block.Kind {
			case sessionentry.BlockToolCall:
				value.Progress.ToolCalls++
			case sessionentry.BlockToolResult:
				value.Progress.OutputBytes += int64(len(block.Text))
			}
		}
	}
	if lastAssistant.StopReason == "completed" || lastAssistant.StopReason == "stop_sequence" {
		value.Progress.FinalAssistantID, value.Progress.FinalStopReason = lastAssistant.ID, lastAssistant.StopReason
	}
	return nil
}
