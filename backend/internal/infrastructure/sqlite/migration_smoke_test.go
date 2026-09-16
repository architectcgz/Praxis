package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationSmoke(t *testing.T) {
	store, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(context.Background())
	var schemaVersion int
	if err := store.DB().QueryRowContext(context.Background(), `SELECT MAX(version) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != 1 {
		t.Fatalf("schema version=%d want=1", schemaVersion)
	}
	var table string
	if err := store.DB().QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'tool_invocations'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"projects", "agent_executions", "tool_invocations", "agent_results", "orchestration_events"} {
		var payloadColumn int
		if err := store.DB().QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'payload'`, table).Scan(&payloadColumn); err != nil {
			t.Fatal(err)
		}
		if payloadColumn != 0 {
			t.Fatalf("table %s still has payload column", table)
		}
	}
	for table, columns := range map[string][]string{
		"session_context_entries": {"id", "content_digest"},
		"agent_executions":        {"context_digest"},
	} {
		for _, column := range columns {
			var count int
			if err := store.DB().QueryRowContext(context.Background(),
				`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("table %s is missing column %s", table, column)
			}
		}
	}
}

func TestOpenRejectsObsoleteVersionOneSchema(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "obsolete.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (1, '2025-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_context_entries (session_id TEXT NOT NULL, revision INTEGER NOT NULL, payload TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(context.Background(), dsn)
	if err == nil || !strings.Contains(err.Error(), "sqlite schema is obsolete") {
		t.Fatalf("error=%v", err)
	}
}
