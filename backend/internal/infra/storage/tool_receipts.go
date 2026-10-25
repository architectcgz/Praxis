package storage

import (
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func validateToolReceipts(records []logRecord, scope string, value entryRecord) error {
	seen := make(map[string]bool)
	for _, block := range value.Data.Content {
		if block.Kind != sessionentry.BlockToolCall && block.Kind != sessionentry.BlockToolResult {
			continue
		}
		if value.Data.ExecutionID == "" || value.Data.TurnID == "" || seen[block.CallID] {
			return errors.New("tool message ownership or call identity is invalid")
		}
		seen[block.CallID] = true
		key := toolexecution.Key{ExecutionID: identity.AgentExecutionID(value.Data.ExecutionID), CallID: block.CallID}
		if block.Kind == sessionentry.BlockToolCall {
			if _, err := getCall(records, key); err == nil {
				return errorcode.ErrRequestConflict
			} else if !errors.Is(err, errorcode.ErrNotFound) {
				return err
			}
			continue
		}
		if _, err := getResult(records, key); err == nil {
			return errorcode.ErrRequestConflict
		} else if !errors.Is(err, errorcode.ErrNotFound) {
			return err
		}
		call, err := getCall(records, key)
		if err != nil {
			return err
		}
		if call.SessionID.String() != scope || call.AgentID != value.AgentID || call.RequestID != value.Data.RequestID || call.TurnID.String() != value.Data.TurnID || string(call.Name) != block.Name {
			return errors.New("tool receipt ownership does not match its declaration")
		}
	}
	return nil
}
