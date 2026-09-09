package sqlite

import (
	"context"
	"testing"
)

func TestMigrationSmoke(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	if inventory, err := store.InspectSchema(context.Background()); err != nil || !inventory.HasRequiredTables() {
		t.Fatalf("inventory=%+v err=%v", inventory, err)
	} else if inventory.SchemaVersion != 1 {
		t.Fatalf("schema version=%d want=1", inventory.SchemaVersion)
	}
	var table string
	if err := store.DB().QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'tool_invocations'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
}
