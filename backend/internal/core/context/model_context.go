package context

import sessionentry "praxis/internal/core/session/entry"

func NewToolResultBlock(callID, name, payload string, isError bool) sessionentry.Block {
	return sessionentry.Block{Kind: sessionentry.BlockToolResult, CallID: callID, Name: name, Text: payload, IsError: isError}
}
