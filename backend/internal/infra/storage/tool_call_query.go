package storage

import (
	"cmp"
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

type ToolCallQueries struct{ storage *Storage }

func getCall(records []logRecord, key toolexecution.Key) (sessionentry.ToolCall, error) {
	for _, record := range records {
		if record.event.Collection != "entry" {
			continue
		}
		value := record.value.(entryRecord)
		for _, call := range value.Data.ToolCalls(identity.SessionID(record.scope), value.AgentID) {
			if call.Key == key {
				return call, nil
			}
		}
	}
	return sessionentry.ToolCall{}, errorcode.ErrNotFound
}

func getResult(records []logRecord, key toolexecution.Key) (sessionentry.ContextEntry, error) {
	for _, record := range records {
		if record.event.Collection != "entry" {
			continue
		}
		value := record.value.(entryRecord)
		if value.Data.ExecutionID != key.ExecutionID.String() {
			continue
		}
		for _, block := range value.Data.Content {
			if block.Kind == sessionentry.BlockToolResult && block.CallID == key.CallID {
				return value.Data.Clone(), nil
			}
		}
	}
	return sessionentry.ContextEntry{}, errorcode.ErrNotFound
}

func (r ToolCallQueries) ListMissingToolResults(ctx context.Context) (values []repository.MissingToolResult, err error) {
	values = make([]repository.MissingToolResult, 0)
	err = withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		results := make(map[toolexecution.Key]bool)
		var calls []repository.MissingToolResult
		for _, record := range records {
			if record.event.Collection != "entry" {
				continue
			}
			value := record.value.(entryRecord)
			index := 0
			for _, block := range value.Data.Content {
				key := toolexecution.Key{ExecutionID: identity.AgentExecutionID(value.Data.ExecutionID), CallID: block.CallID}
				if block.Kind == sessionentry.BlockToolResult {
					results[key] = true
				}
				if block.Kind == sessionentry.BlockToolCall {
					calls = append(calls, repository.MissingToolResult{
						SessionID:   identity.SessionID(record.scope),
						AgentID:     value.AgentID,
						ExecutionID: key.ExecutionID,
						CallID:      key.CallID,
						RequestID:   value.Data.RequestID,
						TurnID:      identity.TurnID(value.Data.TurnID),
						Name:        block.Name,
						Sequence:    value.Data.Sequence,
						CallIndex:   index,
					})
					index++
				}
			}
		}
		for _, call := range calls {
			if !results[toolexecution.Key{ExecutionID: call.ExecutionID, CallID: call.CallID}] {
				values = append(values, call)
			}
		}
		return nil
	})
	return sorted(values, func(a, b repository.MissingToolResult) int {
		return cmp.Or(cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(a.CallIndex, b.CallIndex))
	}), err
}
