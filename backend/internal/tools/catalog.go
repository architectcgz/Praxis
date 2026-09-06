package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

const (
	defaultListDirLimit = 200
	maxListDirLimit     = 1000
)

const listDirSchemaJSON = `{"type":"object","properties":{` +
	`"path":{"type":"string","description":"Directory path, absolute or relative to the execution workspace"},` +
	`"offset":{"type":"integer","minimum":0,"description":"Zero-based entry offset"},` +
	`"limit":{"type":"integer","minimum":1,"maximum":1000,"description":"Maximum entries to return"}` +
	`},"required":["path"],"additionalProperties":false}`

var listDirSchema = json.RawMessage(listDirSchemaJSON)

type Catalog struct{}

func NewCatalog() *Catalog { return &Catalog{} }

func (c *Catalog) Definition(name domainsecurity.ToolName) (runtimecontract.ToolDefinition, bool) {
	if name != domainsecurity.ToolListDir {
		return runtimecontract.ToolDefinition{}, false
	}
	return runtimecontract.ToolDefinition{
		Name: name,
		Description: "List one directory without recursion. Returns sorted JSON entries with type and " +
			"pagination metadata; output is limited to 50 KiB.",
		InputSchema: append(json.RawMessage(nil), listDirSchema...),
	}, true
}

type listDirArguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type listDirInput struct {
	Path   string          `json:"path"`
	Offset json.RawMessage `json:"offset,omitempty"`
	Limit  json.RawMessage `json:"limit,omitempty"`
}

func (c *Catalog) Normalize(
	call runtimecontract.ToolCall,
	invocation runtimecontract.ToolInvocationContext,
) (runtimecontract.AuthorizedToolCall, error) {
	if call.Name != domainsecurity.ToolListDir {
		return runtimecontract.AuthorizedToolCall{}, errors.New("tool is not registered")
	}
	call = call.Snapshot()
	if err := rejectDuplicateListDirFields(call.Input); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir arguments are invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Input))
	decoder.DisallowUnknownFields()
	var input listDirInput
	if err := decoder.Decode(&input); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir arguments are invalid")
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir arguments are invalid")
	}
	arguments := listDirArguments{Path: strings.TrimSpace(input.Path), Limit: defaultListDirLimit}
	if err := decodeOptionalInteger(input.Offset, &arguments.Offset); err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir arguments are invalid")
	}
	if len(input.Limit) > 0 {
		arguments.Limit = 0
		if err := decodeOptionalInteger(input.Limit, &arguments.Limit); err != nil {
			return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir arguments are invalid")
		}
	}
	if arguments.Path == "" || strings.ContainsRune(arguments.Path, '\x00') ||
		arguments.Offset < 0 || arguments.Limit < 1 || arguments.Limit > maxListDirLimit {
		return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir arguments are invalid")
	}
	if !filepath.IsAbs(arguments.Path) {
		arguments.Path = filepath.Join(invocation.Grant.WorkspacePathSnapshot, arguments.Path)
	}
	absolutePath, err := filepath.Abs(arguments.Path)
	if err != nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("list_dir path is invalid")
	}
	arguments.Path = filepath.Clean(absolutePath)
	normalized, err := json.Marshal(arguments)
	if err != nil {
		return runtimecontract.AuthorizedToolCall{}, fmt.Errorf("encode list_dir arguments: %w", err)
	}
	return runtimecontract.AuthorizedToolCall{
		Name:                call.Name,
		NormalizedArguments: normalized,
		Path:                arguments.Path,
		ReadScopes:          append([]string(nil), invocation.Grant.ReadScopes...),
	}, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
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

func decodeOptionalInteger(encoded json.RawMessage, target *int) error {
	if len(encoded) == 0 {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return errors.New("integer cannot be null")
	}
	return json.Unmarshal(encoded, target)
}

func rejectDuplicateListDirFields(encoded json.RawMessage) error {
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
	return rejectTrailingJSON(decoder)
}
