package arguments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"praxis/internal/utils/jsonutil"
	"strings"
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

// PreparePathArguments 校验并规范化使用 path、offset、limit 参数的工具调用。
// 三个 limit 参数指定默认值和有效范围；参数无效时返回错误。
func PreparePathArguments(
	args json.RawMessage,
	workspacePath string,
	defaultLimit int,
	minLimit int,
	maxLimit int,
) (json.RawMessage, error) {
	toolLabel := "read_file"
	invalidArguments := func(reason string) (json.RawMessage, error) {
		return nil, fmt.Errorf("%s arguments are invalid: %s", toolLabel, reason)
	}
	if err := jsonutil.RejectDuplicateFields(args); err != nil {
		return invalidArguments(err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var toolInput pathInput
	if err := decoder.Decode(&toolInput); err != nil {
		return invalidArguments(err.Error())
	}
	if err := jsonutil.RejectTrailingJSON(decoder); err != nil {
		return invalidArguments(err.Error())
	}
	normalizedArguments := pathArguments{Path: toolInput.Path, Limit: defaultLimit}
	if err := jsonutil.DecodeOptionalInteger(toolInput.Offset, &normalizedArguments.Offset); err != nil {
		return invalidArguments("offset: " + err.Error())
	}
	if len(toolInput.Limit) > 0 {
		normalizedArguments.Limit = 0
		if err := jsonutil.DecodeOptionalInteger(toolInput.Limit, &normalizedArguments.Limit); err != nil {
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
	var err error
	normalizedArguments.Path, err = ResolvePath(normalizedArguments.Path, workspacePath)
	if err != nil {
		return invalidArguments(err.Error())
	}
	normalized, err := json.Marshal(normalizedArguments)
	if err != nil {
		return nil, fmt.Errorf("encode %s arguments: %w", toolLabel, err)
	}
	return normalized, nil
}
