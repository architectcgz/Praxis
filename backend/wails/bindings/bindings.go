package bindings

import (
	"bytes"
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"praxis/internal/service"
	"praxis/wails/dto"
)

const agentEventName = "praxis:agent-event"

// Runtime 提供 binding 调用所需的 Wails context 和前端服务集。
type Runtime interface {
	BindingContext() (context.Context, Services, error)
	EventContext() context.Context
	LogError(string, ...any)
}

// Bindings 聚合全部 Wails 暴露对象。
type Bindings struct {
	runtime Runtime

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

// All 返回交给 Wails Bind 的完整对象列表。
func (b *Bindings) All() []interface{} {
	return []interface{}{
		b.projects,
		b.sessions,
		b.agents,
		b.commands,
		b.models,
	}
}

// EmitAgentEvent 把 application 事件转换成专用 DTO 后推给前端。
func (b *Bindings) EmitAgentEvent(event service.AgentEvent) {
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
		value.Usage = &dto.ModelUsage{
			InputTokens:              event.Usage.InputTokens,
			OutputTokens:             event.Usage.OutputTokens,
			CacheReadInputTokens:     event.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: event.Usage.CacheCreationInputTokens,
		}
	}
	wailsruntime.EventsEmit(ctx, agentEventName, value)
}
