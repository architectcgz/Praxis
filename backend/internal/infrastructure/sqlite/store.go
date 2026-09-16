package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"praxis/internal/persistence"
	"praxis/internal/system"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	path    string
}

var migrations = []migration{
	{version: 1, path: "migrations/0001_target.sql"},
}

type transactionContextKey struct{}

// Store owns the SQLite connection and all storage transactions.
type Store struct {
	db    *sql.DB
	clock system.Clock
}

// NewStore wraps an opened SQLite database without changing its schema.
func NewStore(db *sql.DB) (*Store, error) {
	return NewStoreWithClock(db, system.UTCClock{})
}

func NewStoreWithClock(db *sql.DB, clock system.Clock) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlite database is required")
	}
	if clock == nil {
		return nil, errors.New("sqlite clock is required")
	}
	// SQLite foreign-key settings are connection-local. One connection keeps
	// target relation constraints effective for every repository operation.
	db.SetMaxOpenConns(1)
	return &Store{db: db, clock: clock}, nil
}

// Open opens a SQLite database, verifies connectivity, and applies migrations.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("sqlite open context is required")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	store, err := NewStore(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// DB returns the underlying database for composition and health checks.
func (s *Store) DB() *sql.DB { return s.db }

// Now returns the clock value used by SQLite metadata adapters.
func (s *Store) Now() time.Time { return s.clock.Now() }

// Migrate applies the versioned SQLite schema owned by this package.
func (s *Store) Migrate(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite migration context is required")
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable sqlite foreign keys for migration: %w", err)
	}
	if err := s.InTx(ctx, func(ctx context.Context) error {
		executor := executorFromContext(ctx, s.db)
		if _, err := executor.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS schema_migrations (
				version INTEGER PRIMARY KEY,
				applied_at TEXT NOT NULL
			)`); err != nil {
			return fmt.Errorf("create sqlite migration ledger: %w", err)
		}
		for _, migration := range migrations {
			var applied bool
			if err := executor.QueryRowContext(
				ctx,
				`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`,
				migration.version,
			).Scan(&applied); err != nil {
				return fmt.Errorf("inspect sqlite migration %d: %w", migration.version, err)
			}
			if applied {
				continue
			}
			schema, err := migrationFiles.ReadFile(migration.path)
			if err != nil {
				return fmt.Errorf("read sqlite migration %d: %w", migration.version, err)
			}
			if _, err := executor.ExecContext(ctx, string(schema)); err != nil {
				return fmt.Errorf("apply sqlite migration %d: %w", migration.version, err)
			}
			if _, err := executor.ExecContext(
				ctx,
				`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				migration.version,
				s.clock.Now().UTC().Format(time.RFC3339Nano),
			); err != nil {
				return fmt.Errorf("record sqlite migration %d: %w", migration.version, err)
			}
		}
		var currentSchema bool
		if err := executor.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM pragma_table_info('session_context_entries')
				WHERE name = 'id'
			)`).Scan(&currentSchema); err != nil {
			return fmt.Errorf("verify sqlite target schema: %w", err)
		}
		if !currentSchema {
			return errors.New("sqlite schema is obsolete; recreate the database")
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check migrated sqlite foreign keys: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("sqlite migration produced a foreign-key violation")
	}
	return rows.Err()
}

// InTx runs product metadata mutations in one SQLite transaction.
func (s *Store) InTx(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		return errors.New("sqlite transaction context is required")
	}
	if fn == nil {
		return errors.New("sqlite transaction callback is required")
	}
	if _, ok := contextTx(ctx); ok {
		return fn(ctx)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := context.WithValue(ctx, transactionContextKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return nil
}

// Close closes the SQLite connection after honoring an already-cancelled context.
func (s *Store) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite close context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close sqlite database: %w", err)
	}
	return nil
}

var _ persistence.TxRunner = (*Store)(nil)

type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Executor returns the transaction carried by ctx, or the database connection.
func (s *Store) Executor(ctx context.Context) SQLExecutor { return executorFromContext(ctx, s.db) }

// HasTransaction reports whether ctx is inside Store.InTx.
func HasTransaction(ctx context.Context) bool {
	_, ok := contextTx(ctx)
	return ok
}

func executorFromContext(ctx context.Context, db *sql.DB) SQLExecutor {
	if tx, ok := contextTx(ctx); ok {
		return tx
	}
	return db
}

// contextTx returns the active transaction carried by ctx when it is usable.
func contextTx(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(transactionContextKey{}).(*sql.Tx)
	return tx, ok && tx != nil
}

func (s *Store) withValueTx(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	if _, ok := contextTx(ctx); ok {
		return fn(ctx, nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite transaction: %w", err)
	}
	// Roll back any transaction left unfinished by the callback; after Commit,
	// Rollback returns sql.ErrTxDone, which is intentionally ignored.
	defer func() { _ = tx.Rollback() }()
	txCtx := context.WithValue(ctx, transactionContextKey{}, tx)
	if err := fn(txCtx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return nil
}

// nullableTimeValue preserves an unset timestamp as SQL NULL and stores present
// timestamps as UTC RFC3339Nano text.
func nullableTimeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
