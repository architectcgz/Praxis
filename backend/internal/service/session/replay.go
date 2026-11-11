package session

import (
	"context"
	"fmt"
	agentrun "praxis/internal/core/agent/run"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// recoverRuns 只展开索引定位的候选执行，不重放已结束且无缺失回执的正文。
func (s *Service) recoverRuns(ctx context.Context, owner *sessionmodel.Session, log *eventLog, locations []repository.RunLocation, state sessionmodel.SessionState) error {
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
		for _, call := range calls {
			if completed[call.CallID] {
				continue
			}
			if state != sessionmodel.SessionActive || location.EndEntryID != "" {
				return fmt.Errorf("cannot repair tool call %s after a published run end or in an inactive session", call.CallID)
			}
			result := call.ResultEntry(tool.NewToolError("tool_interrupted", "The previous executor exited without a reliable tool result."))
			var stored *sessionentry.ContextEntry
			if record.AgentID == identity.AgentID(record.SessionID) {
				stored, _, err = owner.AppendEntry(ctx, record.BranchID, result)
			} else {
				stored, _, err = owner.AppendSubagentEntry(ctx, record.AgentID, result)
			}
			if err != nil {
				return fmt.Errorf("save interrupted tool result for run %s: %w", id, err)
			}
			entries = append(entries, stored)
		}
		if location.EndEntryID != "" {
			return nil
		}
		if record.CurrentTurn.RequestID != "" {
			requests, err := log.store.Requests(ctx, []identity.TurnID{record.CurrentTurn.ID})
			if err != nil {
				return err
			}
			for _, request := range requests {
				if request.Status == agentrun.ModelRequestRunning {
					request.Status = agentrun.ModelRequestCancelled
					if err := owner.SaveModelRequest(ctx, record, request); err != nil {
						return err
					}
				}
			}
		}
		if record.Active() {
			record.End(agentrun.RunInterrupted, "", s.clock.Now())
		}
		record.Progress = record.Progress.RebuildEntryStats(id.String(), entries)
		_, err = owner.SaveRun(ctx, record)
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

// validateLoadedEvent 只在磁盘载荷读取入口校验，内部使用已确定的记录。
func validateLoadedEvent(event sessionmodel.Event) error {
	var record agentrun.RunRecord
	switch value := event.Payload.(type) {
	case agentrun.RunRecord:
		record = value
	case sessionmodel.RunEnded:
		record = value.Run
		if record.Status != agentrun.RunEnded || !value.Entry.IsRun() {
			return fmt.Errorf("load run %s: invalid end record", record.ID)
		}
	case sessionmodel.ModelRequestSaved:
		record = value.Run
		if err := value.Request.Validate(); err != nil {
			return fmt.Errorf("load model request %s: %w", value.Request.ID, err)
		}
	case sessionentry.ContextEntry:
		if value.ID == "" || value.CreatedAt.IsZero() {
			return fmt.Errorf("load entry: invalid identity or creation time")
		}
		return nil
	}
	if record.ID == "" || record.SessionID == "" || record.AgentID == "" || record.BranchID != event.BranchID {
		return fmt.Errorf("load run: invalid identity or ownership")
	}
	switch record.Status {
	case agentrun.RunStarting, agentrun.RunRunning, agentrun.RunEnding, agentrun.RunEnded:
	default:
		return fmt.Errorf("load run %s: unknown status %q", record.ID, record.Status)
	}
	if record.Status == agentrun.RunEnded {
		switch record.Outcome {
		case agentrun.RunCompleted, agentrun.RunYielded, agentrun.RunFailed, agentrun.RunInterrupted:
		default:
			return fmt.Errorf("load run %s: unknown outcome %q", record.ID, record.Outcome)
		}
	}
	return nil
}
