package storage

import (
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	projectmodel "praxis/internal/core/project"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/infra/jsonl"
	"strings"
)

// decodeStoredEvent 在重放和写入边界校验并解码，内存只保存这份独立业务值。
func decodeStoredEvent(scope string, event jsonl.Event) (any, error) {
	if event.Key == "" || strings.TrimSpace(event.Key) != event.Key {
		return nil, errors.New("invalid object key")
	}
	if (scope == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return nil, errors.New("invalid event file ownership")
	}
	var target any
	var kind string
	switch event.Collection {
	case "project":
		target, kind = new(projectmodel.Project), "project.saved"
	case "workspace":
		target, kind = new(workspacemodel.Workspace), "workspace.saved"
	case "session":
		target, kind = new(sessionmodel.Session), "session.saved"
	case "agent_execution":
		target, kind = new(agentexecution.Execution), "agent_execution.saved"
	case "entry":
		target, kind = new(entryRecord), "entry.appended"
	case "usage":
		target, kind = new(providerapi.UsageRecord), "usage.saved"
	case "wait":
		target, kind = new(workflow.WaitCondition), "wait.saved"
	case "agent_execution_timing":
		target = new(timing.Record)
		if event.Type != "agent_execution.timing_started" && event.Type != "agent_execution.timing_finished" {
			return nil, errors.New("unknown timing event")
		}
		kind = event.Type
	default:
		return nil, fmt.Errorf("unknown stored collection %q", event.Collection)
	}
	if kind != event.Type {
		return nil, errors.New("unknown event type")
	}
	if err := decodeJSON(event.Payload, target); err != nil {
		return nil, err
	}
	var id, owner string
	var value any
	var err error
	switch v := target.(type) {
	case *projectmodel.Project:
		value = *v
		id, err = v.ID.String(), validateProject(*v)
	case *workspacemodel.Workspace:
		value = *v
		id, err = v.ID.String(), validateWorkspace(*v)
	case *sessionmodel.Session:
		value = *v
		id, owner, err = v.ID.String(), v.ID.String(), validateSession(*v)
	case *agentexecution.Execution:
		value = *v
		id, owner, err = v.ID.String(), v.SessionID.String(), validateAgentExecution(*v)
	case *entryRecord:
		value = *v
		id, owner, err = entryKey(v.AgentID, v.Data.ID), scope, v.Validate()
		if v.Data.Sequence != event.Sequence {
			return nil, errors.New("message sequence does not match event sequence")
		}
	case *providerapi.UsageRecord:
		value = *v
		id, owner, err = v.Key(), v.SessionID, validateUsageRecord(*v)
	case *workflow.WaitCondition:
		value = *v
		id, owner, err = v.ID.String(), scope, validateWaitCondition(*v)
	case *timing.Record:
		value = *v
		id, owner, err = timingKey(v.ExecutionID, v.ID), scope, v.Validate()
		if (event.Type == "agent_execution.timing_started") != (v.Status == timing.Running) || v.Status == timing.Interrupted {
			return nil, errors.New("timing state does not match event type")
		}
	}
	if err != nil {
		return nil, err
	}
	if id != event.Key || owner != scope {
		return nil, errors.New("object identity or session ownership does not match event")
	}
	return value, nil
}

// projectRecord 从内存消息派生完成证据和统计，不修改保存的执行记录。
func projectRecord(records []logRecord, target any) error {
	switch value := target.(type) {
	case *agentexecution.Execution:
		value.Progress.OutputBytes = value.Progress.ModelOutputBytes
		value.Progress.ToolCalls = 0
		value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
		var last sessionentry.ContextEntry
		for _, source := range records {
			if source.event.Collection != "entry" || source.scope != value.SessionID.String() {
				continue
			}
			record := source.value.(entryRecord)
			message := record.Data
			if message.ExecutionID != value.ID.String() || record.AgentID != value.AgentID {
				continue
			}
			if message.Role == role.Assistant && message.Sequence > last.Sequence {
				last = message
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
		if last.StopReason == "completed" || last.StopReason == "stop_sequence" {
			value.Progress.FinalAssistantID, value.Progress.FinalStopReason = last.ID, last.StopReason
		}
	case *timing.Record:
		if value.Status != timing.Running {
			return nil
		}
		execution, err := getRecord[agentexecution.Execution](records, "agent_execution", value.ExecutionID)
		if err != nil {
			return err
		}
		if !execution.Active() {
			value.Status = timing.Interrupted
		}
	}
	return nil
}
