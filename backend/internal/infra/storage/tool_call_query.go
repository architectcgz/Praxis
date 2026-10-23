package storage

import (
	"cmp"
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

type ToolCallQueries struct{ s *Store }

func (r ToolCallQueries) GetCall(ctx context.Context, callKey toolexecution.Key) (call sessionentry.ToolCall, err error) {
	err = r.s.view(ctx, func() error {
		call, err = r.getCallLocked(callKey)
		return err
	})
	return call, err
}

func (r ToolCallQueries) getCallLocked(callKey toolexecution.Key) (sessionentry.ToolCall, error) {
	key, err := r.s.lookup(lookupKey{kind: "tool-call", owner: callKey.ExecutionID.String(), value: callKey.CallID})
	if err != nil {
		return sessionentry.ToolCall{}, err
	}
	entry, ok := r.s.entry(key)
	if !ok {
		return sessionentry.ToolCall{}, errorcode.ErrNotFound
	}
	record := entry.entryValue()
	for _, value := range record.Data.ToolCalls(identity.SessionID(entry.scope), record.AgentID) {
		if value.Key == callKey {
			return value, nil
		}
	}
	return sessionentry.ToolCall{}, errorcode.ErrNotFound
}

func (r ToolCallQueries) GetResult(ctx context.Context, key toolexecution.Key) (sessionentry.ContextEntry, error) {
	record, err := getByClaim[entryRecord](ctx, r.s, lookupKey{kind: "tool-result", owner: key.ExecutionID.String(), value: key.CallID})
	return record.Data, err
}

func (r ToolCallQueries) ListMissingToolResults(ctx context.Context) ([]repository.MissingToolResult, error) {
	values := make([]repository.MissingToolResult, 0)
	err := r.s.view(ctx, func() error {
		for _, key := range r.s.keys(lookupKey{kind: "all-tool-call-entries"}) {
			entry, _ := r.s.entry(key)
			record := entry.entryValue()
			callIndex := 0
			for _, block := range record.Data.Content {
				if block.Kind != sessionentry.BlockToolCall {
					continue
				}
				_, err := r.s.lookup(lookupKey{kind: "tool-result", owner: record.Data.ExecutionID, value: block.CallID})
				if errors.Is(err, errorcode.ErrNotFound) {
					values = append(values, repository.MissingToolResult{
						SessionID:   identity.SessionID(entry.scope),
						AgentID:     record.AgentID,
						ExecutionID: identity.AgentExecutionID(record.Data.ExecutionID),
						CallID:      block.CallID,
						RequestID:   record.Data.RequestID,
						TurnID:      identity.TurnID(record.Data.TurnID),
						Name:        block.Name,
						Sequence:    entry.eventSequence,
						CallIndex:   callIndex,
					})
				} else if err != nil {
					return err
				}
				callIndex++
			}
		}
		return nil
	})
	return sorted(values, func(a, b repository.MissingToolResult) int {
		return cmp.Or(cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Sequence, b.Sequence), cmp.Compare(a.CallIndex, b.CallIndex))
	}), err
}
