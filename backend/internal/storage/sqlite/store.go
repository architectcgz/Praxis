package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"praxis/internal/core/persistence"

	_ "modernc.org/sqlite"
)

//go:embed migrations/0001_initial.sql
var migrationFiles embed.FS

type transactionContextKey struct{}

// Store owns the SQLite connection and all storage transactions.
type Store struct {
	db *sql.DB
}

// NewStore wraps an opened SQLite database without changing its schema.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlite database is required")
	}
	return &Store{db: db}, nil
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

// Migrate applies the versioned SQLite schema owned by this package.
func (s *Store) Migrate(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite migration context is required")
	}
	schema, err := migrationFiles.ReadFile("migrations/0001_initial.sql")
	if err != nil {
		return fmt.Errorf("read sqlite migration: %w", err)
	}
	return s.InTx(ctx, func(ctx context.Context) error {
		executor := executorFromContext(ctx, s.db)
		if _, err := executor.ExecContext(ctx, string(schema)); err != nil {
			return fmt.Errorf("apply sqlite migration: %w", err)
		}
		return nil
	})
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

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func executorFromContext(ctx context.Context, db *sql.DB) sqlExecutor {
	if tx, ok := contextTx(ctx); ok {
		return tx
	}
	return db
}

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

func formatTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
