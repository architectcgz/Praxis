package command

import (
	"strings"

	foundation "praxis/internal/core/domain/foundation"
)

type RequestID = foundation.RequestID

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}
