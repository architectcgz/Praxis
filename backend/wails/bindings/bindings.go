package bindings

import (
	"bytes"
	"context"
	coreevent "praxis/internal/core/event"
	"praxis/wails/dto"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const agentEventName = "praxis:agent-event"

type Host interface {
	BindingContext() (context.Context, Services, error)
	EventContext() context.Context
	LogError(string, ...any)
}

type Bindings struct {
	host     Host
	projects *ProjectBindings
	sessions *SessionBindings
	agents   *AgentBindings
	commands *CommandBindings
	models   *ModelBindings
}

func New(host Host) *Bindings {
	return &Bindings{
		host:     host,
		projects: &ProjectBindings{host: host},
		sessions: &SessionBindings{host: host},
		agents:   &AgentBindings{host: host},
		commands: &CommandBindings{host: host},
		models:   &ModelBindings{host: host},
	}
}

func (b *Bindings) All() []any {
	return []any{b.projects, b.sessions, b.agents, b.commands, b.models}
}

// EmitAgentEvent 将 Core 业务通知转为前端协议；Entry 直接沿用核心类型。
func (b *Bindings) EmitAgentEvent(event coreevent.Event) {
	if event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	ctx := b.host.EventContext()
	if ctx == nil {
		return
	}
	value := dto.AgentEvent{
		Kind:           string(event.Kind),
		Entries:        event.Entries,
		SessionID:      event.SessionID.String(),
		AgentID:        event.AgentID.String(),
		ExecutionID:    event.ExecutionID.String(),
		Outcome:        event.Outcome,
		FailureCode:    event.FailureCode,
		FailureMessage: event.FailureMessage,
		TurnID:         event.TurnID.String(),
		Text:           event.Text,
		CallID:         event.CallID,
		Name:           event.Name,
		Input:          bytes.Clone(event.Input),
		Result:         event.Result,
		IsError:        event.IsError,
		Error:          event.Error,
	}
	if event.Timing != nil {
		timing := operationTiming(*event.Timing)
		value.Timing = &timing
	}
	if event.Usage != nil {
		usage := publicUsage(*event.Usage)
		value.Usage = &usage
	}
	wailsruntime.EventsEmit(ctx, agentEventName, value)
}
