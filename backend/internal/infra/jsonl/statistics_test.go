package jsonl

import (
	"context"
	"errors"
	"testing"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
	"praxis/internal/timing"
)

func TestUsageAndTimingIndexesSurviveRestart(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	ctx := context.Background()
	output := int64(5)
	usage := model.ModelUsageRecord{
		SessionID: "session",
		AgentID:   "agent",
		TaskID:    task.ID.String(),
		TurnID:    contracts.NewTurnID(task.ID, 1),
		Usage:     model.ModelUsage{InputTokens: 10, OutputTokens: &output},
	}
	requireOK(t, (ModelUsageStore{Store: f.store}).Save(ctx, usage))
	decreased := usage
	decreased.Usage.OutputTokens = nil
	if err := (ModelUsageStore{Store: f.store}).Save(ctx, decreased); err == nil {
		t.Fatal("known usage must not be lost")
	}
	record := timing.Record{
		ID: "timing",
		Operation: timing.Operation{
			SessionID: "session",
			AgentID:   "agent",
			TaskID:    task.ID.String(),
			Kind:      timing.Provider,
			Name:      "provider",
		},
		StartedAt: time.Now().UTC(),
		Status:    timing.Running,
		Revision:  1,
	}
	requireOK(t, (TimingStore{Store: f.store}).Save(ctx, record))
	f.restart(t)
	values, err := (ModelUsageStore{Store: f.store}).ListSession(ctx, "session")
	requireOK(t, err)
	if len(values) != 1 || values[0].Usage.InputTokens != 10 || *values[0].Usage.OutputTokens != 5 {
		t.Fatal(values)
	}
	timings := TimingStore{Store: f.store}
	requireOK(t, timings.RecoverInterrupted(ctx))
	entries, err := timings.ListAgent(ctx, "agent", 0)
	requireOK(t, err)
	if len(entries) != 1 || entries[0].Status != timing.Interrupted || entries[0].Revision != 2 || entries[0].DurationMS != nil {
		t.Fatal(entries)
	}
	if err := timings.Save(ctx, record); err == nil {
		t.Fatal("timing version must not regress")
	}
	f.restart(t)
	if _, err := f.repos.Tasks.FindByRequest(ctx, "agent", "missing"); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatal(err)
	}
}
