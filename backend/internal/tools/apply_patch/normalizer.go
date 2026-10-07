package applypatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"
	toolshared "praxis/internal/tools/shared"
)

type arguments struct {
	Patch string `json:"patch"`
	Path  string `json:"path"`
}

// Normalize 校验补丁格式并规范化补丁工作区根目录。
func Normalize(call contracts.ToolCall) (toolcontracts.NormalizedToolCall, error) {
	invalidArguments := func(reason string) (toolcontracts.NormalizedToolCall, error) {
		return toolcontracts.NormalizedToolCall{}, fmt.Errorf("apply_patch arguments are invalid: %s", reason)
	}
	if call.Name != toolcontracts.ToolApplyPatch {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool is not registered")
	}
	if err := toolshared.RejectDuplicateFields(call.Arguments); err != nil {
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
	if err := toolshared.RejectTrailingJSON(decoder); err != nil {
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
		normalized.Path = filepath.Clean(raw.Path)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, fmt.Errorf("encode apply_patch arguments: %w", err)
	}
	return toolcontracts.NormalizedToolCall{
		Name:                call.Name,
		NormalizedArguments: encoded,
		Path:                normalized.Path,
	}, nil
}
