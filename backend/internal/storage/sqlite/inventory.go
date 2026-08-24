package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var legacyTableNames = []string{
	"task_sessions",
	"agent_threads",
	"agent_runs",
	"work_queue_items",
	"briefing_deliveries",
}

var targetTableNames = []string{
	"sessions",
	"agent_groups",
	"agents",
	"agent_executions",
	"queued_work_items",
	"wait_conditions",
	"agent_control_requests",
	"context_deliveries",
}

// SchemaInventory is a metadata-only view used before and after conversion.
// It contains table names and counts, never JSONL, prompt, or artifact bodies.
type SchemaInventory struct {
	SchemaVersion    int
	LegacyTables     []string
	LegacyCounts     map[string]int
	TargetTables     []string
	LegacyQueueItems int
	TargetCounts     map[string]int
}

// InspectSchema reports the durable schema without loading aggregate payloads.
func (s *Store) InspectSchema(ctx context.Context) (SchemaInventory, error) {
	return s.inspectSchema(ctx)
}

// InspectExisting opens a database only long enough to inspect its current
// tables. It does not apply migrations, making it suitable for production
// DataRoot preflight before target schema creation.
func InspectExisting(ctx context.Context, dsn string) (SchemaInventory, error) {
	if ctx == nil {
		return SchemaInventory{}, errors.New("sqlite existing schema context is required")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return SchemaInventory{}, fmt.Errorf("open sqlite for schema inspection: %w", err)
	}
	store, err := NewStore(db)
	if err != nil {
		_ = db.Close()
		return SchemaInventory{}, err
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return SchemaInventory{}, fmt.Errorf("ping sqlite for schema inspection: %w", err)
	}
	return store.inspectSchema(ctx)
}

func (s *Store) inspectSchema(ctx context.Context) (SchemaInventory, error) {
	if ctx == nil {
		return SchemaInventory{}, errors.New("sqlite schema inventory context is required")
	}
	if err := ctx.Err(); err != nil {
		return SchemaInventory{}, err
	}
	existing, err := s.existingTables(ctx)
	if err != nil {
		return SchemaInventory{}, err
	}
	version := 0
	if existing["schema_migrations"] {
		if err := executorFromContext(ctx, s.db).QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
		).Scan(&version); err != nil {
			return SchemaInventory{}, fmt.Errorf("read sqlite schema version: %w", err)
		}
	}
	inventory := SchemaInventory{
		SchemaVersion: version,
		LegacyTables:  make([]string, 0),
		LegacyCounts:  make(map[string]int),
		TargetTables:  make([]string, 0),
		TargetCounts:  make(map[string]int),
	}
	for _, table := range legacyTableNames {
		if !existing[table] {
			continue
		}
		inventory.LegacyTables = append(inventory.LegacyTables, table)
		var count int
		if err := executorFromContext(ctx, s.db).QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM `+table,
		).Scan(&count); err != nil {
			return SchemaInventory{}, fmt.Errorf("count legacy table %s: %w", table, err)
		}
		inventory.LegacyCounts[table] = count
		if table == "work_queue_items" {
			inventory.LegacyQueueItems = count
		}
	}
	for _, table := range targetTableNames {
		if !existing[table] {
			continue
		}
		inventory.TargetTables = append(inventory.TargetTables, table)
		var count int
		if err := executorFromContext(ctx, s.db).QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM `+table,
		).Scan(&count); err != nil {
			return SchemaInventory{}, fmt.Errorf("count sqlite table %s: %w", table, err)
		}
		inventory.TargetCounts[table] = count
	}
	return inventory, nil
}

// HasLegacyData reports whether any known legacy table contains rows. Empty
// legacy tables are part of the bootstrap migration and do not block startup.
func (i SchemaInventory) HasLegacyData() bool {
	for _, count := range i.LegacyCounts {
		if count > 0 {
			return true
		}
	}
	return false
}

// VerifyTargetIntegrity checks SQLite's relation constraints and target table
// presence after conversion. It intentionally does not inspect payload bodies.
func (s *Store) VerifyTargetIntegrity(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite target integrity context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, table := range targetTableNames {
		var exists bool
		if err := executorFromContext(ctx, s.db).QueryRowContext(
			ctx,
			`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`,
			table,
		).Scan(&exists); err != nil {
			return fmt.Errorf("inspect target table %s: %w", table, err)
		}
		if !exists {
			return fmt.Errorf("target table %s is missing", table)
		}
	}
	rows, err := executorFromContext(ctx, s.db).QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run sqlite foreign-key check: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, rowID, parent, foreignKey sql.NullString
		if err := rows.Scan(&table, &rowID, &parent, &foreignKey); err != nil {
			return fmt.Errorf("scan sqlite foreign-key check: %w", err)
		}
		return fmt.Errorf("sqlite foreign-key violation in %s row %s", table.String, rowID.String)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite foreign-key check: %w", err)
	}
	return nil
}

func (s *Store) existingTables(ctx context.Context) (map[string]bool, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(
		ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table'`,
	)
	if err != nil {
		return nil, fmt.Errorf("list sqlite tables: %w", err)
	}
	defer rows.Close()
	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan sqlite table: %w", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite tables: %w", err)
	}
	return tables, nil
}
