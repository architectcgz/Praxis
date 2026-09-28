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

// RejectDuplicateFields 要求 JSON 对象中的字段名只能出现一次。
func RejectDuplicateFields(encoded json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("arguments must be an object")
	}
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return errors.New("argument name is invalid")
		}
		if _, exists := seen[name]; exists {
			return errors.New("duplicate argument")
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	return RejectTrailingJSON(decoder)
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
