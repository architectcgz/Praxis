package session

import (
	"context"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// LogStore 只管理主、子代理日志及其完整行，不解释业务载荷。
type LogStore interface {
	Read(context.Context, identity.SessionID, identity.AgentID, func([]byte) error) error
	Append(context.Context, identity.SessionID, identity.AgentID, []byte) error
	CreateSubagent(context.Context, identity.SessionID, identity.AgentID) error
	Subagents(context.Context, identity.SessionID) ([]identity.AgentID, error)
}

type eventLog struct {
	store LogStore
}

var _ sessionmodel.EventLog = eventLog{}

func (l eventLog) Read(ctx context.Context, id identity.SessionID, agentID identity.AgentID, apply func(sessionmodel.Event) error) error {
	return l.store.Read(ctx, id, agentID, func(line []byte) error {
		event, err := decodeEvent(line)
		if err != nil {
			return err
		}
		if event.AgentID != agentID {
			return fmt.Errorf("event agent %q does not match log agent %q", event.AgentID, agentID)
		}
		switch payload := event.Payload.(type) {
		case agentexecution.ExecutionRecord:
			if err := validateStoredExecution(payload, id, agentID); err != nil {
				return err
			}
		case sessionmodel.ModelRequestFinished:
			if err := validateStoredExecution(payload.Execution, id, agentID); err != nil {
				return err
			}
			if payload.Request.ExecutionID != payload.Execution.ID {
				return fmt.Errorf("model request %s execution does not match snapshot %s", payload.Request.TurnID, payload.Execution.ID)
			}
			if err := payload.Request.Validate(); err != nil {
				return err
			}
		}
		return apply(event)
	})
}

func (l eventLog) AppendEvent(ctx context.Context, id identity.SessionID, event sessionmodel.Event) error {
	if event.AgentID == "" {
		event.AgentID = identity.AgentID(id)
	}
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	return l.store.Append(ctx, id, event.AgentID, data)
}

func (l eventLog) CreateSubagent(ctx context.Context, id identity.SessionID, agentID identity.AgentID) error {
	return l.store.CreateSubagent(ctx, id, agentID)
}

func (l eventLog) Subagents(ctx context.Context, id identity.SessionID) ([]identity.AgentID, error) {
	return l.store.Subagents(ctx, id)
}

func validateStoredExecution(record agentexecution.ExecutionRecord, sessionID identity.SessionID, agentID identity.AgentID) error {
	if record.SessionID != sessionID || record.AgentID != agentID {
		return fmt.Errorf("execution %s does not belong to its log", record.ID)
	}
	return record.Validate()
}
