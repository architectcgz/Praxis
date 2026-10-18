package applypatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/tool"
	toolargs "praxis/internal/tool/arguments"
	"strings"
)

type arguments struct {
	Patch string `json:"patch"`
	Path  string `json:"path"`
}

// Normalize 校验补丁格式并规范化补丁工作区根目录。
func Normalize(call tool.ToolCall, workspacePath string) (tool.NormalizedToolCall, error) {
	invalidArguments := func(reason string) (tool.NormalizedToolCall, error) {
		return tool.NormalizedToolCall{}, fmt.Errorf("apply_patch arguments are invalid: %s", reason)
	}
	if call.Name != tool.ToolApplyPatch {
		return tool.NormalizedToolCall{}, errors.New("tool is not registered")
	}
	if err := toolargs.RejectDuplicateFields(call.Arguments); err != nil {
		return invalidArguments(err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	var raw struct {
		Patch string `json:"patch"`
		Path  string `json:"path"`
	}
	if err := decoder.Decode(&raw); err != nil {
		return invalidArguments(err.Error())
	}
	if err := toolargs.RejectTrailingJSON(decoder); err != nil {
		return invalidArguments(err.Error())
	}
	if raw.Patch == "" {
		return invalidArguments("patch is required")
	}
	if len([]byte(raw.Patch)) > maxPatchBytes {
		return invalidArguments("patch is too large")
	}
	if strings.ContainsRune(raw.Path, '\x00') {
		return invalidArguments("path contains a null character")
	}
	if _, err := parsePatch(raw.Patch); err != nil {
		return invalidArguments(err.Error())
	}

	normalized := arguments{Patch: raw.Patch, Path: "."}
	if strings.TrimSpace(raw.Path) != "" {
		normalized.Path = raw.Path
	}
	var err error
	normalized.Path, err = toolargs.ResolvePath(normalized.Path, workspacePath)
	if err != nil {
		return invalidArguments(err.Error())
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return tool.NormalizedToolCall{}, fmt.Errorf("encode apply_patch arguments: %w", err)
	}
	return tool.NormalizedToolCall{
		Name:                call.Name,
		NormalizedArguments: encoded,
		Path:                normalized.Path,
	}, nil
}
