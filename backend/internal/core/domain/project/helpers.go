package project

import (
	"path/filepath"
	"strings"

	foundation "praxis/internal/core/domain/foundation"
)

type (
	ProjectID   = foundation.ProjectID
	WorkspaceID = foundation.WorkspaceID
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func invalidTransition(entity, from, to string) error {
	return &foundation.TransitionError{Entity: entity, From: from, To: to}
}

func normalizeAbsolutePath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}
