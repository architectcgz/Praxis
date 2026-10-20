package bash

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	toolargs "praxis/internal/infra/tools/arguments"
	"praxis/internal/utils/jsonutil"
	"strings"
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

// ApprovalCommand 从已准备的参数中提取命令，不执行规范化或审批决策。
func ApprovalCommand(args json.RawMessage) (string, error) {
	var prepared arguments
	if err := json.Unmarshal(args, &prepared); err != nil || prepared.Command == "" {
		return "", errors.New("prepared bash command is required")
	}
	return prepared.Command, nil
}

// Prepare 校验 bash 参数，并将工作目录交给统一路径解析流程处理。
func Prepare(args json.RawMessage, workspacePath string) (json.RawMessage, error) {
	invalidArguments := func(reason string) (json.RawMessage, error) {
		return nil, fmt.Errorf("bash arguments are invalid: %s", reason)
	}
	if err := jsonutil.RejectDuplicateFields(args); err != nil {
		return invalidArguments(err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	var raw input
	if err := decoder.Decode(&raw); err != nil {
		return invalidArguments(err.Error())
	}
	if err := jsonutil.RejectTrailingJSON(decoder); err != nil {
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
		normalized.Path = raw.Path
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
	var err error
	normalized.Path, err = toolargs.ResolvePath(normalized.Path, workspacePath)
	if err != nil {
		return invalidArguments(err.Error())
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode bash arguments: %w", err)
	}
	return encoded, nil
}
