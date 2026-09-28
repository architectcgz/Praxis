package context

import (
	"praxis/internal/contracts"
)

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneContextEntryIDs(values []contracts.ContextEntryID) []contracts.ContextEntryID {
	if values == nil {
		return nil
	}
	return append([]contracts.ContextEntryID(nil), values...)
}
