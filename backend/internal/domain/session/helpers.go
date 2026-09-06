package session

import (
	"strings"

	foundation "praxis/internal/domain/foundation"
)

type (
	ProjectID   = foundation.ProjectID
	SessionID   = foundation.SessionID
	WorkspaceID = foundation.WorkspaceID
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func invalidTransition(entity, from, to string) error {
	return &foundation.TransitionError{Entity: entity, From: from, To: to}
}
