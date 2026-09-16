package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
)

func TestEventRepositoryStoresRelationalAuditRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(ctx) }()

	var documentColumns int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('orchestration_events') WHERE name IN ('document_ref', 'payload')`).Scan(&documentColumns); err != nil {
		t.Fatal(err)
	}
	if documentColumns != 0 {
		t.Fatalf("orchestration_events still has a document column: %d", documentColumns)
	}

	at := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	kinds := []string{"decision", "reference", "user_message"}
	err = store.InTx(ctx, func(txCtx context.Context) error {
		for index, kind := range kinds {
			event := domainfoundation.DomainEvent{
				ID:              domainfoundation.EventID(fmt.Sprintf("event_%d", index+1)),
				Type:            domainfoundation.EventSessionContextAppended,
				OccurredAt:      at.Add(time.Duration(index) * time.Second),
				SessionID:       "session_1",
				ContextRevision: uint64(index + 1),
				ContextKind:     kind,
			}
			if err := store.AppendEvent(txCtx, event); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := store.ListSessionEvents(ctx, "session_1", time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].ContextKind != "decision" || events[2].ContextRevision != 3 {
		t.Fatalf("unexpected event list: %#v", events)
	}

	page, err := store.ListSessionEvents(ctx, "session_1", events[0].OccurredAt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ContextRevision != 2 {
		t.Fatalf("unexpected paged events: %#v", page)
	}

	err = store.InTx(ctx, func(txCtx context.Context) error {
		return store.AppendEvent(txCtx, events[0])
	})
	if !errors.Is(err, domainfoundation.ErrRequestConflict) {
		t.Fatalf("duplicate event id should conflict, got %v", err)
	}

	err = store.InTx(ctx, func(txCtx context.Context) error {
		return store.AppendEvent(txCtx, domainfoundation.DomainEvent{
			ID: "event_bad", Type: domainfoundation.EventProjectCreated, OccurredAt: at,
		})
	})
	if err == nil {
		t.Fatal("incomplete event should be rejected")
	}
}
