package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
)

func targetLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func loadTargetPayload[T any](
	ctx context.Context,
	store *Store,
	query string,
	args []any,
	name string,
	validate func(T) error,
) (T, error) {
	var zero T
	row := executorFromContext(ctx, store.db).QueryRowContext(ctx, query, args...)
	var payload []byte
	if err := row.Scan(&payload); errors.Is(err, sql.ErrNoRows) {
		return zero, domain.ErrNotFound
	} else if err != nil {
		return zero, fmt.Errorf("read %s: %w", name, err)
	}
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return zero, fmt.Errorf("decode stored %s: %w", name, err)
	}
	if err := validate(value); err != nil {
		return zero, fmt.Errorf("validate stored %s: %w", name, err)
	}
	return value, nil
}

func listTargetPayloads[T any](
	ctx context.Context,
	store *Store,
	query string,
	args []any,
	name string,
	validate func(T) error,
) ([]T, error) {
	rows, err := executorFromContext(ctx, store.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", name, err)
	}
	defer rows.Close()
	values := make([]T, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}
		var value T
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode stored %s: %w", name, err)
		}
		if err := validate(value); err != nil {
			return nil, fmt.Errorf("validate stored %s: %w", name, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", name, err)
	}
	return values, nil
}
