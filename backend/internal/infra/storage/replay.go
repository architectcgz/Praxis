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
	"praxis/internal/core/tool"
	"praxis/internal/core/workflow"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"strings"
)

// replayEvent 是日志格式到类型化状态的唯一转换边界，只校验记录自身及文件身份。
func replayEvent(state *state, scope jsonl.Scope, event jsonl.Event) error {
	if event.Key == "" || strings.TrimSpace(event.Key) != event.Key {
		return errors.New("invalid object key")
	}
	if (scope.ID == "") != (event.Collection == "project" || event.Collection == "workspace") {
		return errors.New("invalid event file ownership")
	}
	sessionID := identity.SessionID(scope.ID)
	switch event.Collection {
	case "project":
		var value projectmodel.Project
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "project.saved", value.ID.String(), "", scope.ID); err != nil {
			return err
		}
		state.projects[value.ID] = value
	case "workspace":
		var value workspacemodel.Workspace
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "workspace.saved", value.ID.String(), "", scope.ID); err != nil {
			return err
		}
		state.workspaces[value.ID] = value
	case "session":
		var value sessionmodel.Session
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "session.saved", value.ID.String(), value.ID.String(), scope.ID); err != nil {
			return err
		}
		if value.ProjectID.String() != scope.ProjectID {
			return errors.New("session project does not match log directory")
		}
		state.sessions[value.ID] = value
	case "agent_execution":
		var value agentexecution.ExecutionRecord
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "agent_execution.saved", value.ID.String(), value.SessionID.String(), scope.ID); err != nil {
			return err
		}
		state.executions[value.ID] = value
	case "entry":
		if event.Type == "entry.updated" {
			var update toolResultUpdate
			if err := jsonl.Decode(event.Payload, &update); err != nil {
				return err
			}
			for _, field := range []string{update.AgentID.String(), update.EntryID} {
				if field == "" || field != strings.TrimSpace(field) || strings.ContainsAny(field, "\x00\r\n") {
					return errors.New("tool result update identity must be canonical and nonempty")
				}
			}
			if len(update.Text) > tool.MaxInlineResultBytes {
				return errors.New("tool result update exceeds the inline size limit")
			}
			key := entryKey{AgentID: update.AgentID, ID: update.EntryID}
			if err := eventIdentity(event, "entry.updated", encodedEntryKey(key), scope.ID, scope.ID); err != nil {
				return err
			}
			prior, ok := state.entries[key]
			if !ok {
				return errors.New("updated entry is missing its original append event")
			}
			data := &prior.Record.Data
			if prior.SessionID != sessionID || data.Role != role.Tool || len(data.Content) != 1 || data.Content[0].Kind != sessionentry.BlockToolResult {
				return errors.New("tool result update requires an existing tool receipt in the same session")
			}
			data.Content[0].Text = update.Text
			prior.UpdatedSequence = event.Sequence
			state.entries[key] = prior
		} else {
			var value entryRecord
			if err := jsonl.Decode(event.Payload, &value); err != nil {
				return err
			}
			if err := value.Validate(); err != nil {
				return err
			}
			key := entryKey{AgentID: value.AgentID, ID: value.Data.ID}
			if err := eventIdentity(event, "entry.appended", encodedEntryKey(key), scope.ID, scope.ID); err != nil {
				return err
			}
			if value.Data.Sequence != event.Sequence {
				return errors.New("message sequence does not match event sequence")
			}
			if _, exists := state.entries[key]; exists {
				return errors.New("duplicate entry append identity")
			}
			state.entries[key] = storedEntry{
				SessionID:       sessionID,
				Record:          value,
				UpdatedSequence: event.Sequence,
			}
			state.entryOrder[sessionID] = append(state.entryOrder[sessionID], key)
		}
	case "context":
		var value contextRecord
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "context.saved", scope.ID, scope.ID, scope.ID); err != nil {
			return err
		}
		if value.ThroughSequence != event.Sequence-1 {
			return errors.New("saved context boundary does not match event sequence")
		}
		for _, entry := range value.Entries {
			if entry.Sequence >= event.Sequence {
				return errors.New("saved context entry must precede its save event")
			}
		}
		state.contexts[sessionID] = value
	case "usage":
		var value providerapi.UsageRecord
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "usage.saved", value.Key(), value.SessionID, scope.ID); err != nil {
			return err
		}
		state.usage[value.Key()] = value
	case "wait":
		var value workflow.WaitCondition
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		if err := eventIdentity(event, "wait.saved", value.ID.String(), scope.ID, scope.ID); err != nil {
			return err
		}
		state.waits[value.ID] = storedWait{SessionID: sessionID, Value: value}
	case "agent_execution_timing":
		var value timing.Record
		if err := jsonl.Decode(event.Payload, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		typ := "agent_execution.timing_finished"
		if value.Status == timing.Running {
			typ = "agent_execution.timing_started"
		}
		if value.Status == timing.Interrupted {
			return errors.New("interrupted timing is a read projection")
		}
		if err := eventIdentity(event, typ, timingKey(value.ExecutionID, value.ID), scope.ID, scope.ID); err != nil {
			return err
		}
		state.timings[timingKey(value.ExecutionID, value.ID)] = value
	default:
		return fmt.Errorf("unknown stored collection %q", event.Collection)
	}
	state.sequences[scope.ID] = event.Sequence
	return nil
}

func eventIdentity(event jsonl.Event, typ, key, owner, scope string) error {
	if event.Type != typ {
		return errors.New("unknown event type")
	}
	if event.Key != key || owner != scope {
		return errors.New("object identity or session ownership does not match event")
	}
	return nil
}
