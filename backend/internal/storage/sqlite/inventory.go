package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var requiredTables = []string{
	"projects",
	"workspaces",
	"sessions",
	"agent_groups",
	"agents",
	"task_packets",
	"context_manifests",
	"capability_grants",
	"delegation_requests",
	"agent_executions",
	"queued_work_items",
	"wait_conditions",
	"agent_control_requests",
	"context_deliveries",
	"workspace_write_leases",
	"agent_results",
	"briefings",
	"notes",
	"orchestration_events",
}

// SchemaInventory is a metadata-only view of the target database.
type SchemaInventory struct {
	SchemaVersion int
	Tables        []string
	Counts        map[string]int
}

func (s *Store) InspectSchema(ctx context.Context) (SchemaInventory, error) {
	return s.inspectSchema(ctx)
}

// InspectExisting opens a database without applying migrations.
func InspectExisting(ctx context.Context, dsn string) (SchemaInventory, error) {
	if ctx == nil {
		return SchemaInventory{}, errors.New("sqlite schema inspection context is required")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return SchemaInventory{}, fmt.Errorf("open sqlite for schema inspection: %w", err)
	}
	defer db.Close()
	store, err := NewStore(db)
	if err != nil {
		return SchemaInventory{}, err
	}
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
	tables, err := s.existingTables(ctx)
	if err != nil {
		return SchemaInventory{}, err
	}
	version := 0
	if tables["schema_migrations"] {
		if err := executorFromContext(ctx, s.db).QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
		).Scan(&version); err != nil {
			return SchemaInventory{}, fmt.Errorf("read sqlite schema version: %w", err)
		}
	}
	inventory := SchemaInventory{SchemaVersion: version, Tables: make([]string, 0), Counts: make(map[string]int)}
	for _, table := range requiredTables {
		if !tables[table] {
			continue
		}
		inventory.Tables = append(inventory.Tables, table)
		var count int
		if err := executorFromContext(ctx, s.db).QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return SchemaInventory{}, fmt.Errorf("count sqlite table %s: %w", table, err)
		}
		inventory.Counts[table] = count
	}
	return inventory, nil
}

func (i SchemaInventory) HasRequiredTables() bool {
	if len(i.Tables) != len(requiredTables) {
		return false
	}
	seen := make(map[string]struct{}, len(i.Tables))
	for _, table := range i.Tables {
		seen[table] = struct{}{}
	}
	for _, table := range requiredTables {
		if _, ok := seen[table]; !ok {
			return false
		}
	}
	return true
}

func (s *Store) VerifyTargetIntegrity(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite integrity context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tables, err := s.existingTables(ctx)
	if err != nil {
		return err
	}
	for _, table := range requiredTables {
		if !tables[table] {
			return fmt.Errorf("target table %s is missing", table)
		}
	}
	rows, err := executorFromContext(ctx, s.db).QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run sqlite foreign-key check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, rowID, parent, foreignKey sql.NullString
		if err := rows.Scan(&table, &rowID, &parent, &foreignKey); err != nil {
			return fmt.Errorf("scan sqlite foreign-key check: %w", err)
		}
		return fmt.Errorf("sqlite foreign-key violation in %s row %s", table.String, rowID.String)
	}
	return rows.Err()
}

func (s *Store) existingTables(ctx context.Context) (map[string]bool, error) {
	rows, err := executorFromContext(ctx, s.db).QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
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
