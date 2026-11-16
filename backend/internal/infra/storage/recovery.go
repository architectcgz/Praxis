package storage

import (
	"context"
	"fmt"
	"praxis/internal/clock"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// recoverRuns 只展开索引定位的候选执行，不重放已结束且无缺失回执的正文。
func recoverRuns(ctx context.Context, owner *sessionmodel.Session, log *entryLog, locations []repository.RunLocation, state sessionmodel.SessionState, clock clock.Clock) error {
	ids, err := log.store.RecoveryRuns(ctx)
	if err != nil {
		return err
	}
	wanted := make(map[identity.RunID]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	candidates := make(map[identity.RunID]repository.RunLocation)
	children := make(map[identity.RunID][]identity.RunID)
	for _, location := range locations {
		if wanted[location.Record.ID] {
			candidates[location.Record.ID] = location
			children[location.Record.ParentRunID] = append(children[location.Record.ParentRunID], location.Record.ID)
		}
	}
	var finish func(identity.RunID) error
	finish = func(id identity.RunID) error {
		location := candidates[id]
		for _, child := range children[id] {
			if err := finish(child); err != nil {
				return err
			}
		}
		record, err := log.readRun(ctx, location)
		if err != nil {
			return err
		}
		entryIDs, err := log.store.RunEntries(ctx, id)
		if err != nil {
			return err
		}
		entries, err := log.Entries(ctx, entryIDs)
		if err != nil {
			return err
		}
		calls := make([]sessionentry.ToolCall, 0)
		completed := make(map[string]bool)
		for _, entry := range entries {
			calls = append(calls, entry.ToolCalls(record.SessionID, record.AgentID)...)
			for _, block := range entry.Content {
				if result, ok := block.(sessionentry.ToolResultBlock); ok {
					completed[result.CallID] = true
				}
			}
		}
		batches := make(map[identity.TurnID][]sessionentry.ContentBlock)
		var turns []identity.TurnID
		for _, call := range calls {
			if completed[call.CallID] {
				continue
			}
			if state != sessionmodel.SessionActive || location.EndEntryID != "" {
				return fmt.Errorf("cannot repair tool call %s after a published run end or in an inactive session", call.CallID)
			}
			if _, ok := batches[call.TurnID]; !ok {
				turns = append(turns, call.TurnID)
			}
			result := call.ResultEntry(tool.NewToolError("tool_interrupted", "The previous executor exited without a reliable tool result."))
			batches[call.TurnID] = append(batches[call.TurnID], result.Content...)
		}
		for _, turn := range turns {
			value := sessionentry.ContextEntry{
				ID:         "tools:" + turn.String(),
				RunID:      record.ID.String(),
				TurnID:     turn.String(),
				CreatedAt:  clock.Now(),
				Role:       role.Tool,
				AuthorKind: sessionentry.AuthorTool,
				Content:    batches[turn],
			}
			if _, _, err := owner.BindHistory(record.AgentID)(ctx, value); err != nil {
				return err
			}
		}

		if location.EndEntryID != "" {
			return nil
		}

		for _, request := range log.store.index.requests {
			if request.RunID == record.ID && request.Status == sessionentry.RequestRunning {
				request.Status = sessionentry.RequestInterrupted
				if _, _, err := owner.BindHistory(record.AgentID)(ctx, sessionentry.ModelRequestEntry(request, nil)); err != nil {
					return err
				}
			}
		}

		if record.CurrentTurn.ID != "" && record.CurrentTurn.EndedAt.IsZero() {
			turn := record.CurrentTurn
			turn.EndedAt = clock.Now()
			if _, _, err := owner.BindHistory(record.AgentID)(ctx, sessionentry.ContextEntry{
				ID:      turn.ID.String() + ":end",
				RunID:   id.String(),
				TurnID:  turn.ID.String(),
				Payload: sessionentry.TurnInterrupted{Turn: turn},
			}); err != nil {
				return err
			}
		}
		record, err = log.readRun(ctx, location)
		if err != nil {
			return err
		}
		result := sessionentry.RunResult{
			Outcome:          string(sessionentry.OutcomeInterrupted),
			FailureCode:      string(sessionentry.RunFailureInterrupted),
			StartedAt:        record.StartedAt,
			EndedAt:          clock.Now(),
			Usage:            record.Progress.Usage,
			ModelRequests:    record.Progress.ModelRequests,
			Iterations:       record.Progress.Iterations,
			OutputBytes:      record.Progress.OutputBytes,
			ToolCalls:        record.Progress.ToolCalls,
			FinalAssistantID: record.Progress.FinalAssistantID,
			FinalStopReason:  record.Progress.FinalStopReason,
		}
		_, _, err = owner.BindHistory(record.AgentID)(ctx, sessionentry.ContextEntry{
			ID:        "run:" + record.ID.String(),
			RunID:     record.ID.String(),
			CreatedAt: clock.Now(),
			Payload:   sessionentry.RunInterrupted{Result: result},
		})
		return err

	}
	for id, location := range candidates {
		if !wanted[location.Record.ParentRunID] {
			if err := finish(id); err != nil {
				return err
			}
		}
	}
	return nil
}
