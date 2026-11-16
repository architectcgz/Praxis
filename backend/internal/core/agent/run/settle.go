package run

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
)

func (r *Run) ConfirmRunning(ctx context.Context) error {
	record := r.Snapshot()
	at := r.deps.Clock.Now()
	_, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: "started:" + record.ID.String(), Payload: sessionentry.RunStarted{At: at}})
	if err == nil {
		r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(""), Payload: coreevent.RunStarted{Run: r.Snapshot().Snapshot()}})
	}
	return err
}

func (r *Run) Settle(ctx context.Context, outcome sessionentry.RunOutcome, code sessionentry.RunFailureCode, message string) (sessionentry.RunRecord, error) {
	record := r.Snapshot()
	result := sessionentry.RunResult{
		Outcome:          string(outcome),
		FailureCode:      string(code),
		FailureMessage:   message,
		StartedAt:        record.StartedAt,
		EndedAt:          r.deps.Clock.Now(),
		Usage:            record.Progress.Usage,
		ModelRequests:    record.Progress.ModelRequests,
		Iterations:       record.Progress.Iterations,
		OutputBytes:      record.Progress.OutputBytes,
		ToolCalls:        record.Progress.ToolCalls,
		FinalAssistantID: record.Progress.FinalAssistantID,
		FinalStopReason:  record.Progress.FinalStopReason,
	}
	var payload sessionentry.Payload
	switch outcome {
	case sessionentry.OutcomeCompleted:
		payload = sessionentry.RunCompleted{Result: result}
	case sessionentry.OutcomeYielded:
		payload = sessionentry.SubagentYielded{Result: result}
	case sessionentry.OutcomeFailed:
		payload = sessionentry.RunFailed{Result: result}
	case sessionentry.OutcomeInterrupted:
		payload = sessionentry.RunInterrupted{Result: result}
	}
	stored, _, err := r.AppendEntry(ctx, sessionentry.ContextEntry{ID: "run:" + record.ID.String(), Payload: payload})
	if err == nil {
		r.mu.Lock()
		r.terminalSaved = true
		r.mu.Unlock()
		if hookErr := r.deps.HookDispatcher.RunCommitted(context.WithoutCancel(ctx), hook.RunEvent{Identity: runIdentity(r, ""), Outcome: string(outcome), FailureCode: string(code)}); hookErr != nil {
			r.deps.Logger.Errorf("run committed hook failed: %v", hookErr)
		}
		if r.Main() && outcome == sessionentry.OutcomeCompleted {
			r.nameSession(ctx)
		}
	}
	if err == nil {
		r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(""), Payload: coreevent.RunEnded{Run: r.Snapshot().Snapshot(), State: r.Snapshot().AgentState(), Entries: []sessionentry.ContextEntry{*stored}}})
	}
	return r.Snapshot(), err
}

func (r *Run) nameSession(ctx context.Context) {
	record := r.Snapshot()
	info, err := r.deps.Sessions.Get(ctx, record.SessionID)
	if err != nil || info.Title != "" {
		return
	}
	input, ok := r.Entry(record.InputEntryID)
	if !ok {
		return
	}
	title := sessionmodel.TitleFromInput(input.TextContent())
	if title != "" {
		if err := r.deps.Sessions.UpdateTitle(ctx, info.ID, info.Title, title, r.deps.Clock.Now()); err != nil {
			r.deps.Logger.Errorf("name session: %v", err)
		}
	}
}
