package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"
)

type pathArguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type pathInput struct {
	Path   string          `json:"path"`
	Offset json.RawMessage `json:"offset,omitempty"`
	Limit  json.RawMessage `json:"limit,omitempty"`
}

// NormalizePathArguments 校验并规范化使用 path、offset、limit 参数的工具调用。
// name 指定工具名称，三个 limit 参数指定默认值和有效范围；参数无效时返回错误。
func NormalizePathArguments(
	call contracts.ToolCall,
	name toolcontracts.ToolName,
	defaultLimit int,
	minLimit int,
	maxLimit int,
) (toolcontracts.NormalizedToolCall, error) {
	toolLabel := string(name)
	invalidArguments := func(reason string) (toolcontracts.NormalizedToolCall, error) {
		return toolcontracts.NormalizedToolCall{}, fmt.Errorf("%s arguments are invalid: %s", toolLabel, reason)
	}
	if call.Name != name {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool is not registered")
	}
	if err := RejectDuplicateFields(call.Arguments); err != nil {
		return invalidArguments(err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	var toolInput pathInput
	if err := decoder.Decode(&toolInput); err != nil {
		return invalidArguments(err.Error())
	}
	if err := RejectTrailingJSON(decoder); err != nil {
		return invalidArguments(err.Error())
	}
	normalizedArguments := pathArguments{Path: toolInput.Path, Limit: defaultLimit}
	if err := DecodeOptionalInteger(toolInput.Offset, &normalizedArguments.Offset); err != nil {
		return invalidArguments("offset: " + err.Error())
	}
	if len(toolInput.Limit) > 0 {
		normalizedArguments.Limit = 0
		if err := DecodeOptionalInteger(toolInput.Limit, &normalizedArguments.Limit); err != nil {
			return invalidArguments("limit: " + err.Error())
		}
	}
	if strings.TrimSpace(normalizedArguments.Path) == "" {
		return invalidArguments("path is required")
	}
	if strings.ContainsRune(normalizedArguments.Path, '\x00') {
		return invalidArguments("path contains a null character")
	}
	if normalizedArguments.Offset < 0 {
		return invalidArguments("offset cannot be negative")
	}
	if normalizedArguments.Limit < minLimit || normalizedArguments.Limit > maxLimit {
		return invalidArguments(fmt.Sprintf("limit must be between %d and %d", minLimit, maxLimit))
	}
	normalizedArguments.Path = filepath.Clean(normalizedArguments.Path)
	normalized, err := json.Marshal(normalizedArguments)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, fmt.Errorf("encode %s arguments: %w", toolLabel, err)
	}
	return toolcontracts.NormalizedToolCall{
		Name:                call.Name,
		NormalizedArguments: normalized,
		Path:                normalizedArguments.Path,
	}, nil
}
