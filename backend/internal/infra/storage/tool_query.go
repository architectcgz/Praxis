package storage

import (
	"cmp"
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

type ToolCallQueries struct{ storage *Store }

func (r ToolCallQueries) ListMissingToolResults(ctx context.Context) (values []repository.MissingToolResult, err error) {
	values = make([]repository.MissingToolResult, 0)
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	results := make(map[toolexecution.Key]bool)
	var calls []repository.MissingToolResult
	for _, stored := range r.storage.state.entries {
		value := stored.Record
		index := 0
		for _, block := range value.Data.Content {
			key := toolexecution.Key{ExecutionID: identity.AgentExecutionID(value.Data.ExecutionID), CallID: block.CallID}
			if block.Kind == sessionentry.BlockToolResult {
				results[key] = true
			}
			if block.Kind == sessionentry.BlockToolCall {
				calls = append(calls, repository.MissingToolResult{
					SessionID:   stored.SessionID,
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
	return sorted(values, func(a, b repository.MissingToolResult) int {
		return cmp.Or(cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(a.CallIndex, b.CallIndex))
	}), err
}
