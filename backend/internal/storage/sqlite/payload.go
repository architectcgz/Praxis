package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domainfoundation "praxis/internal/core/domain/foundation"
)

func (s *Store) loadPayload(ctx context.Context, table, id string, target any, validate func() error) error {
	row := executorFromContext(ctx, s.db).QueryRowContext(ctx, `SELECT payload FROM `+table+` WHERE id = ?`, id)
	var payload []byte
	if err := row.Scan(&payload); errors.Is(err, sql.ErrNoRows) {
		return domainfoundation.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode stored %s: %w", table, err)
	}
	if err := validate(); err != nil {
		return fmt.Errorf("validate stored %s: %w", table, err)
	}
	return nil
}

func decodePayload[T any](row *sql.Row, validate func(T) error) (T, error) {
	var zero T
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return zero, err
	}
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return zero, err
	}
	if err := validate(value); err != nil {
		return zero, err
	}
	return value, nil
}

func encodePayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode product payload: %w", err)
	}
	var checked any
	if err := json.Unmarshal(payload, &checked); err != nil {
		return nil, fmt.Errorf("inspect product payload: %w", err)
	}
	if hasSensitiveKey(checked) {
		return nil, errors.New("refusing to persist a payload with a sensitive field")
	}
	return payload, nil
}

func hasSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "apikey",
				"api_key",
				"authorization",
				"secret",
				"token",
				"providerpayload",
				"provider_payload",
				"hiddenprompt",
				"hidden_prompt":
				return true
			}
			if hasSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}

func (s *Store) savePayload(ctx context.Context, query string, args ...any) error {
	if _, ok := contextTx(ctx); !ok {
		return errors.New("product mutation requires sqlite transaction")
	}
	if _, err := executorFromContext(ctx, s.db).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("persist product payload: %w", err)
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
