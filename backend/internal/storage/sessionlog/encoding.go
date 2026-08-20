package sessionlog

import (
	"encoding/json"
	"errors"
	"strings"
)

func unmarshalEntryPayload(contents []byte, target any) error {
	if len(contents) == 0 {
		return errors.New("session entry payload is empty")
	}
	return json.Unmarshal(contents, target)
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "apikey",
				"api_key",
				"authorization",
				"secret",
				"token",
				"providerpayload",
				"provider_payload",
				"hiddenprompt",
				"hidden_prompt":
				return true
			}
			if containsSensitiveValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveValue(child) {
				return true
			}
		}
	case string:
		lower := strings.ToLower(typed)
		return strings.Contains(lower, "authorization:") || strings.Contains(lower, "bearer ") ||
			strings.Contains(lower, "api_key=")
	}
	return false
}
