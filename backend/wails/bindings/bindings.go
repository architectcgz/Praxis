package bindings

import (
	"bytes"
	"context"

	"praxis/internal/execution"
	"praxis/wails/dto"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const agentEventName = "praxis:agent-event"

type Runtime interface {
	BindingContext() (context.Context, Services, error)
	EventContext() context.Context
	LogError(string, ...any)
}

type Bindings struct {
	runtime  Runtime
	projects *ProjectBindings
	sessions *SessionBindings
	agents   *AgentBindings
	commands *CommandBindings
	models   *ModelBindings
}

func New(runtime Runtime) *Bindings {
	return &Bindings{
		runtime:  runtime,
		projects: &ProjectBindings{runtime: runtime},
		sessions: &SessionBindings{runtime: runtime},
		agents:   &AgentBindings{runtime: runtime},
		commands: &CommandBindings{runtime: runtime},
		models:   &ModelBindings{runtime: runtime},
	}
}

func (b *Bindings) All() []any {
	return []any{b.projects, b.sessions, b.agents, b.commands, b.models}
}

// EmitAgentEvent 在协议出口复制可变字段，不向前端暴露业务对象。
func (b *Bindings) EmitAgentEvent(event execution.Event) {
	if event.AgentID == "" || event.TaskID == "" {
		return
	}
	ctx := b.runtime.EventContext()
	if ctx == nil {
		return
	}
	value := dto.AgentEvent{
		Kind:           event.Kind,
		SessionID:      event.SessionID.String(),
		AgentID:        event.AgentID.String(),
		TaskID:         event.TaskID.String(),
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
