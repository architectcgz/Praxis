package app

import (
	"praxis/internal/agentruntime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const agentOutputEventName = "praxis:agent-output"

func (a *App) emitAgentOutput(event agentruntime.AgentOutputEvent) {
	if event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	ctx := a.runtime.eventContext()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, agentOutputEventName, event)
}
