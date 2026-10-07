package bash

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"
	toolshared "praxis/internal/tools/shared"
)

const (
	maxCommandBytes = 128 * 1024
	defaultTimeout  = 120
	maxTimeout      = 600
)

type arguments struct {
	Command string  `json:"command"`
	Path    string  `json:"path"`
	Timeout float64 `json:"timeout,omitempty"`
}

type input struct {
	Command string          `json:"command"`
	Path    string          `json:"path"`
	Timeout json.RawMessage `json:"timeout,omitempty"`
}

// Normalize 校验 bash 参数，并将工作目录交给统一路径解析流程处理。
func Normalize(call contracts.ToolCall) (toolcontracts.NormalizedToolCall, error) {
	invalidArguments := func(reason string) (toolcontracts.NormalizedToolCall, error) {
		return toolcontracts.NormalizedToolCall{}, fmt.Errorf("bash arguments are invalid: %s", reason)
	}
	if call.Name != toolcontracts.ToolBash {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool is not registered")
	}
	if err := toolshared.RejectDuplicateFields(call.Arguments); err != nil {
		return invalidArguments(err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	var raw input
	if err := decoder.Decode(&raw); err != nil {
		return invalidArguments(err.Error())
	}
	if err := toolshared.RejectTrailingJSON(decoder); err != nil {
		return invalidArguments(err.Error())
	}
	if strings.TrimSpace(raw.Command) == "" {
		return invalidArguments("command is required")
	}
	if len([]byte(raw.Command)) > maxCommandBytes {
		return invalidArguments("command is too long")
	}
	if strings.ContainsRune(raw.Path, '\x00') {
		return invalidArguments("path contains a null character")
	}

	normalized := arguments{Command: raw.Command, Path: ".", Timeout: defaultTimeout}
	if strings.TrimSpace(raw.Path) != "" {
		normalized.Path = filepath.Clean(raw.Path)
	}
	if len(raw.Timeout) > 0 {
		if bytes.Equal(bytes.TrimSpace(raw.Timeout), []byte("null")) {
			return invalidArguments("timeout cannot be null")
		}
		if err := json.Unmarshal(raw.Timeout, &normalized.Timeout); err != nil ||
			math.IsNaN(normalized.Timeout) || math.IsInf(normalized.Timeout, 0) ||
			normalized.Timeout <= 0 || normalized.Timeout > maxTimeout {
			return invalidArguments("timeout must be greater than 0 and within the supported range")
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, fmt.Errorf("encode bash arguments: %w", err)
	}
	return toolcontracts.NormalizedToolCall{
		Name:                call.Name,
		NormalizedArguments: encoded,
		Path:                normalized.Path,
	}, nil
}
