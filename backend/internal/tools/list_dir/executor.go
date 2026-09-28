package listdir

import (
	toolmodel "praxis/internal/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type entry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type output struct {
	Path       string  `json:"path"`
	Offset     int     `json:"offset"`
	Limit      int     `json:"limit"`
	Total      int     `json:"total"`
	Entries    []entry `json:"entries"`
	NextOffset *int    `json:"nextOffset,omitempty"`
}

// Execute lists the authorized directory and returns one bounded result page.
func Execute(ctx context.Context, call toolcontracts.AuthorizedToolCall) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if call.Name != toolcontracts.ToolListDir {
		return toolcontracts.ToolResult{}, errors.New("list_dir executor does not support the call")
	}
	call = call.Snapshot()
	var toolArguments arguments
	if err := json.Unmarshal(call.NormalizedArguments, &toolArguments); err != nil {
		return toolcontracts.ToolResult{}, errors.New("normalized list_dir arguments are invalid")
	}
	if toolArguments.Path != call.Path || !authorizedRealDirectory(toolArguments.Path, call.ReadScopes) {
		return toolcontracts.ToolResult{}, errors.New("list_dir path is outside an authorized directory")
	}
	entries, err := os.ReadDir(toolArguments.Path)
	if err != nil {
		return toolcontracts.ToolResult{}, errors.New("list_dir could not read the directory")
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	if toolArguments.Offset > len(entries) {
		toolArguments.Offset = len(entries)
	}
	end := toolArguments.Offset + toolArguments.Limit
	if end > len(entries) {
		end = len(entries)
	}
	toolOutput := output{
		Path: toolArguments.Path, Offset: toolArguments.Offset, Limit: toolArguments.Limit,
		Total: len(entries), Entries: make([]entry, 0, end-toolArguments.Offset),
	}
	truncated := false
	for index := toolArguments.Offset; index < end; index++ {
		if err := ctx.Err(); err != nil {
			return toolcontracts.ToolResult{}, err
		}
		toolEntry := entry{Name: entries[index].Name(), Type: directoryEntryType(entries[index])}
		toolOutput.Entries = append(toolOutput.Entries, toolEntry)
		next := index + 1
		if next < len(entries) {
			toolOutput.NextOffset = &next
		}
		if encoded, err := json.Marshal(toolOutput); err != nil {
			return toolcontracts.ToolResult{}, errors.New("list_dir could not encode its result")
		} else if len(encoded) > toolmodel.MaxInlineToolResultBytes {
			toolOutput.Entries = toolOutput.Entries[:len(toolOutput.Entries)-1]
			end = index
			next = index
			toolOutput.NextOffset = &next
			truncated = true
			break
		}
	}
	if end < len(entries) {
		next := end
		toolOutput.NextOffset = &next
		truncated = true
	} else {
		toolOutput.NextOffset = nil
	}
	encoded, err := json.Marshal(toolOutput)
	if err != nil {
		return toolcontracts.ToolResult{}, errors.New("list_dir could not encode its result")
	}
	if len(encoded) > toolmodel.MaxInlineToolResultBytes {
		return toolcontracts.ToolResult{}, errors.New("list_dir result metadata exceeds the size limit")
	}
	return toolcontracts.NewToolSuccess(string(encoded), truncated), nil
}

func directoryEntryType(directoryEntry os.DirEntry) string {
	mode := directoryEntry.Type()
	if mode&os.ModeSymlink != 0 {
		return "symlink"
	}
	if directoryEntry.IsDir() {
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
