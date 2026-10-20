package applypatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	toolargs "praxis/internal/infra/tools/arguments"
	"praxis/internal/utils/jsonutil"
	"strings"
)

type arguments struct {
	Patch string `json:"patch"`
	Path  string `json:"path"`
}

// Prepare 校验补丁格式并规范化补丁工作区根目录。
func Prepare(args json.RawMessage, workspacePath string) (json.RawMessage, error) {
	invalidArguments := func(reason string) (json.RawMessage, error) {
		return nil, fmt.Errorf("apply_patch arguments are invalid: %s", reason)
	}
	if err := jsonutil.RejectDuplicateFields(args); err != nil {
		return invalidArguments(err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var raw struct {
		Patch string `json:"patch"`
		Path  string `json:"path"`
	}
	if err := decoder.Decode(&raw); err != nil {
		return invalidArguments(err.Error())
	}
	if err := jsonutil.RejectTrailingJSON(decoder); err != nil {
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
		return nil, fmt.Errorf("encode apply_patch arguments: %w", err)
	}
	return encoded, nil
}
