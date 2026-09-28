package bindings

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	agentruntime "praxis/internal/runtime/agent"
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

// EmitAgentEvent 把瞬时 Agent runtime 事件作为 Wails 事件推给前端。
func (b *Bindings) EmitAgentEvent(event agentruntime.AgentEvent) {
	if event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	ctx := b.runtime.EventContext()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, agentEventName, event)
}
