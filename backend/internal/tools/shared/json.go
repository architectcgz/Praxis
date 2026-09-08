// Package shared provides reusable helpers for tool implementations.
package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// DecodeOptionalInteger decodes an omitted or integer JSON field into target.
// JSON null is rejected so callers can distinguish an omitted field from an
// explicit invalid value before applying their default.
func DecodeOptionalInteger(encoded json.RawMessage, target *int) error {
	if len(encoded) == 0 {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return errors.New("integer cannot be null")
	}
	return json.Unmarshal(encoded, target)
}

// RejectTrailingJSON verifies that the decoder consumed exactly one JSON value.
func RejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values are not allowed")
	}
	return err
}
