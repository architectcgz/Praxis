package storage

import (
	"praxis/internal/contracts"

	"context"
	"database/sql"
	"errors"
	"fmt"
)

func targetLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func loadDocumentRef[T any](
	ctx context.Context,
	store *Store,
	query string,
	args []any,
	name string,
	validate func(T) error,
) (T, error) {
	var zero T
	row := store.executor(ctx).QueryRowContext(ctx, query, args...)
	var ref string
	if err := row.Scan(&ref); errors.Is(err, sql.ErrNoRows) {
		return zero, contracts.ErrNotFound
	} else if err != nil {
		return zero, fmt.Errorf("read %s: %w", name, err)
	}
	var value T
	if err := store.loadDocument(ctx, ref, &value, name, func() error { return validate(value) }); err != nil {
		return zero, err
	}
	return value, nil
}

func listDocumentRefs[T any](
	ctx context.Context,
	store *Store,
	query string,
	args []any,
	name string,
	validate func(T) error,
) ([]T, error) {
	rows, err := store.executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", name, err)
	}
	defer rows.Close()
	values := make([]T, 0)
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}
		var value T
		if err := store.loadDocument(ctx, ref, &value, name, func() error { return validate(value) }); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", name, err)
	}
	return values, nil
}
