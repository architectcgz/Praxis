package readfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	domainexecution "praxis/internal/domain/execution"
	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
	toolshared "praxis/internal/tools/shared"
)

const (
	defaultLimit = 32 * 1024
	minLimit     = utf8.UTFMax
	maxLimit     = domainexecution.MaxInlineToolResultBytes
)

type arguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type input struct {
	Path   string          `json:"path"`
	Offset json.RawMessage `json:"offset,omitempty"`
	Limit  json.RawMessage `json:"limit,omitempty"`
}

// Normalize validates the read_file input and resolves its path within the
// execution workspace before it is admitted for execution.
func Normalize(
	call runtimecontract.ToolCall,
	invocation runtimecontract.ToolInvocationContext,
) (runtimecontract.AuthorizedToolCall, error) {
	if call.Name != domainsecurity.ToolReadFile {
		return runtimecontract.AuthorizedToolCall{}, errors.New("tool is not registered")
	}
	call = call.Snapshot()
	if err := rejectDuplicateFields(call.Input); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("read_file arguments are invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Input))
	decoder.DisallowUnknownFields()
	var toolInput input
	if err := decoder.Decode(&toolInput); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("read_file arguments are invalid")
	}
	if err := toolshared.RejectTrailingJSON(decoder); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("read_file arguments are invalid")
	}
	normalizedArguments := arguments{Path: strings.TrimSpace(toolInput.Path), Limit: defaultLimit}
	if err := toolshared.DecodeOptionalInteger(toolInput.Offset, &normalizedArguments.Offset); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("read_file arguments are invalid")
	}
	if len(toolInput.Limit) > 0 {
		normalizedArguments.Limit = 0
		if err := toolshared.DecodeOptionalInteger(toolInput.Limit, &normalizedArguments.Limit); err != nil {
			return runtimecontract.AuthorizedToolCall{}, errors.New("read_file arguments are invalid")
		}
	}
	if normalizedArguments.Path == "" || strings.ContainsRune(normalizedArguments.Path, '\x00') ||
		normalizedArguments.Offset < 0 || normalizedArguments.Limit < minLimit || normalizedArguments.Limit > maxLimit {
		return runtimecontract.AuthorizedToolCall{}, errors.New("read_file arguments are invalid")
	}
	if !filepath.IsAbs(normalizedArguments.Path) {
		normalizedArguments.Path = filepath.Join(invocation.Grant.WorkspacePathSnapshot, normalizedArguments.Path)
	}
	absolutePath, err := filepath.Abs(normalizedArguments.Path)
	if err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("read_file path is invalid")
	}
	normalizedArguments.Path = filepath.Clean(absolutePath)
	normalized, err := json.Marshal(normalizedArguments)
	if err != nil {
		return runtimecontract.AuthorizedToolCall{}, fmt.Errorf("encode read_file arguments: %w", err)
	}
	return runtimecontract.AuthorizedToolCall{
		Name:                call.Name,
		NormalizedArguments: normalized,
		Path:                normalizedArguments.Path,
		ReadScopes:          append([]string(nil), invocation.Grant.ReadScopes...),
	}, nil
}

func rejectDuplicateFields(encoded json.RawMessage) error {
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
	return toolshared.RejectTrailingJSON(decoder)
}
