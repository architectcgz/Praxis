package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	domainexecution "praxis/internal/domain/execution"
	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

type Executor struct{}

func NewExecutor() *Executor { return &Executor{} }

func (e *Executor) Execute(
	ctx context.Context,
	call runtimecontract.AuthorizedToolCall,
) (runtimecontract.ToolResult, error) {
	if ctx == nil {
		return runtimecontract.ToolResult{}, errors.New("tool execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return runtimecontract.ToolResult{}, err
	}
	if call.Name != domainsecurity.ToolListDir {
		return runtimecontract.ToolResult{}, errors.New("tool executor does not support the call")
	}
	return executeListDir(ctx, call.Snapshot())
}

type listDirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type listDirOutput struct {
	Path       string         `json:"path"`
	Offset     int            `json:"offset"`
	Limit      int            `json:"limit"`
	Total      int            `json:"total"`
	Entries    []listDirEntry `json:"entries"`
	NextOffset *int           `json:"nextOffset,omitempty"`
}

func executeListDir(ctx context.Context, call runtimecontract.AuthorizedToolCall) (runtimecontract.ToolResult, error) {
	var arguments listDirArguments
	if err := json.Unmarshal(call.NormalizedArguments, &arguments); err != nil {
		return runtimecontract.ToolResult{}, errors.New("normalized list_dir arguments are invalid")
	}
	if arguments.Path != call.Path || !authorizedRealDirectory(arguments.Path, call.ReadScopes) {
		return runtimecontract.ToolResult{}, errors.New("list_dir path is outside an authorized directory")
	}
	entries, err := os.ReadDir(arguments.Path)
	if err != nil {
		return runtimecontract.ToolResult{}, errors.New("list_dir could not read the directory")
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	if arguments.Offset > len(entries) {
		arguments.Offset = len(entries)
	}
	end := arguments.Offset + arguments.Limit
	if end > len(entries) {
		end = len(entries)
	}
	output := listDirOutput{
		Path: arguments.Path, Offset: arguments.Offset, Limit: arguments.Limit,
		Total: len(entries), Entries: make([]listDirEntry, 0, end-arguments.Offset),
	}
	truncated := false
	for index := arguments.Offset; index < end; index++ {
		if err := ctx.Err(); err != nil {
			return runtimecontract.ToolResult{}, err
		}
		entry := listDirEntry{Name: entries[index].Name(), Type: directoryEntryType(entries[index])}
		output.Entries = append(output.Entries, entry)
		next := index + 1
		if next < len(entries) {
			output.NextOffset = &next
		}
		if encoded, err := json.Marshal(output); err != nil {
			return runtimecontract.ToolResult{}, errors.New("list_dir could not encode its result")
		} else if len(encoded) > domainexecution.MaxInlineToolResultBytes {
			output.Entries = output.Entries[:len(output.Entries)-1]
			end = index
			next = index
			output.NextOffset = &next
			truncated = true
			break
		}
	}
	if end < len(entries) {
		next := end
		output.NextOffset = &next
		truncated = true
	} else {
		output.NextOffset = nil
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return runtimecontract.ToolResult{}, errors.New("list_dir could not encode its result")
	}
	if len(encoded) > domainexecution.MaxInlineToolResultBytes {
		return runtimecontract.ToolResult{}, errors.New("list_dir result metadata exceeds the size limit")
	}
	return runtimecontract.ToolResult{Content: string(encoded), Truncated: truncated}, nil
}

func directoryEntryType(entry os.DirEntry) string {
	mode := entry.Type()
	if mode&os.ModeSymlink != 0 {
		return "symlink"
	}
	if entry.IsDir() {
		return "directory"
	}
	if mode.IsRegular() {
		return "file"
	}
	return "other"
}

func authorizedRealDirectory(path string, scopes []string) bool {
	cleanPath := filepath.Clean(path)
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil || !samePath(cleanPath, realPath) {
		return false
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.IsDir() {
		return false
	}
	for _, scope := range scopes {
		cleanScope := filepath.Clean(scope)
		if !pathWithin(cleanScope, cleanPath) {
			continue
		}
		realScope, err := filepath.EvalSymlinks(cleanScope)
		if err == nil && samePath(cleanScope, realScope) && pathWithin(realScope, realPath) {
			return true
		}
	}
	return false
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
