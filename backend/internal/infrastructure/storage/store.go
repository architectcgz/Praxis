package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	"praxis/internal/infrastructure/document"
	"praxis/internal/infrastructure/sqlite"
)

// Store composes relational metadata with file-backed aggregate documents.
type Store struct {
	db        *sqlite.Store
	documents *document.Store
}

// New creates the storage adapter used by the composition root.
func New(db *sqlite.Store, documents *document.Store) (*Store, error) {
	if db == nil || documents == nil {
		return nil, errors.New("storage database and document store are required")
	}
	return &Store{db: db, documents: documents}, nil
}

func (s *Store) executor(ctx context.Context) sqlite.SQLExecutor { return s.db.Executor(ctx) }

func nullableID(id fmt.Stringer) any {
	if id == nil || id.String() == "" {
		return nil
	}
	return id.String()
}

func nullableTimeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func (s *Store) putDocument(ctx context.Context, collection, id string, value any) (string, error) {
	if !sqlite.HasTransaction(ctx) {
		return "", errors.New("product mutation requires sqlite transaction")
	}
	return s.documents.Put(ctx, collection, id, value)
}

func (s *Store) loadDocument(
	ctx context.Context,
	ref string,
	target any,
	name string,
	validate func() error,
) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("%s document reference is empty", name)
	}
	if err := s.documents.Get(ctx, ref, target); errors.Is(err, os.ErrNotExist) {
		return domainfoundation.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read %s document: %w", name, err)
	}
	if validate != nil {
		if err := validate(); err != nil {
			return fmt.Errorf("validate stored %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) execMutation(ctx context.Context, query string, args ...any) error {
	if !sqlite.HasTransaction(ctx) {
		return errors.New("product mutation requires sqlite transaction")
	}
	if _, err := s.executor(ctx).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("persist sqlite metadata: %w", err)
	}
	return nil
}

func exactlyOne(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check %s: %w", operation, err)
	}
	if affected != 1 {
		return domainfoundation.ErrNotFound
	}
	return nil
}

func isConstraintError(err error, column string) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed: "+column)
}

func (s *Store) saveMetadata(ctx context.Context, query string, args ...any) error {
	return s.execMutation(ctx, query, args...)
}

func (s *Store) loadDocumentByID(ctx context.Context, table, id string, target any, validate func() error) error {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM `+table+` WHERE id = ?`, id)
	var ref string
	if err := row.Scan(&ref); errors.Is(err, sql.ErrNoRows) {
		return domainfoundation.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	return s.loadDocument(ctx, ref, target, table, validate)
}
