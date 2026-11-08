package context

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
)

func NewToolResultBlock(callID, name, payload string, isError bool) sessionentry.ToolResultBlock {
	return sessionentry.ToolResultBlock{
		CallID:  callID,
		Name:    tool.ToolName(name),
		Text:    payload,
		IsError: isError,
	}
}
