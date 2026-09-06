package context

import (
	"strings"

	foundation "praxis/internal/domain/foundation"
)

type (
	AgentExecutionID  = foundation.AgentExecutionID
	ContextManifestID = foundation.ContextManifestID
	SessionID         = foundation.SessionID
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func fmtField(field string, err error) error {
	if err == nil {
		return nil
	}
	return invalidValue(field, strings.TrimPrefix(err.Error(), field+": "))
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneContentRefs(values []ContentRef) []ContentRef {
	if values == nil {
		return nil
	}
	return append([]ContentRef(nil), values...)
}
