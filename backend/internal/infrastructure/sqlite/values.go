package sqlite

import (
	"database/sql"
	"fmt"
	"time"
)

func targetLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

// nullableID converts any domain ID with a String method to a SQL text value,
// preserving an empty ID as NULL for nullable foreign-key columns.
func nullableID(id fmt.Stringer) any {
	if id == nil || id.String() == "" {
		return nil
	}
	return id.String()
}

func parseTimestamp(value, name string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s timestamp: %w", name, err)
	}
	return parsed, nil
}

func parseNullableTimestamp(value sql.NullString, name string) (time.Time, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}
	return parseTimestamp(value.String, name)
}
