package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func exactlyOne(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check %s: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%s: row not found", operation)
	}
	return nil
}

func (s *Store) execMutation(ctx context.Context, query string, args ...any) error {
	if !HasTransaction(ctx) {
		return errors.New("product mutation requires sqlite transaction")
	}
	if _, err := s.Executor(ctx).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("persist sqlite metadata: %w", err)
	}
	return nil
}

func isConstraintError(err error, column string) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed: "+column)
}
