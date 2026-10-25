package storage

import (
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"reflect"
	"slices"
	"strings"
)

// validateSession 在磁盘输入边界拒绝非法会话，不修改持久化字段。
func validateSession(value sessionmodel.Session) error {
	for _, id := range []string{
		value.ID.String(),
		value.ProjectID.String(),
		value.WorkspaceID.String(),
		value.MainAgentID.String(),
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("session: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if !definitionIDPattern.MatchString(value.AgentDefinitionID.String()) {
		return fmt.Errorf("session.agentDefinitionID: invalid agent definition identity: %w", errorcode.ErrInvalidValue)
	}
	if value.State != sessionmodel.SessionActive && value.State != sessionmodel.SessionArchived {
		return fmt.Errorf("session.state: unknown session state: %w", errorcode.ErrInvalidValue)
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return fmt.Errorf("session.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }
func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
