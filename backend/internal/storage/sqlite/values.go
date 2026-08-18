package sqlite

import "fmt"

// nullableID converts any domain ID with a String method to a SQL text value,
// preserving an empty ID as NULL for nullable foreign-key columns.
func nullableID(id fmt.Stringer) any {
	if id == nil || id.String() == "" {
		return nil
	}
	return id.String()
}
