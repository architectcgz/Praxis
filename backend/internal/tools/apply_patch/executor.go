package applypatch

import (
	toolmodel "praxis/internal/core/tool_invocation"
	toolcontracts "praxis/internal/tools/contracts"
	"praxis/internal/utils/pathutil"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

type fileChange struct {
	path      string
	content   []byte
	mode      fs.FileMode
	delete    bool
	existed   bool
	temporary string
	backup    string
	installed bool
}

type fileResult struct {
	Path   string `json:"path"`
	From   string `json:"from,omitempty"`
	Action string `json:"action"`
}

type output struct {
	Files []fileResult `json:"files"`
}

// Execute 在授权写入范围内校验并应用完整补丁。
func Execute(ctx context.Context, call toolcontracts.AuthorizedToolCall) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool turn context is required")
	}
	if err := ctx.Err(); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if call.Name != toolcontracts.ToolApplyPatch {
		return toolcontracts.ToolResult{}, errors.New("apply_patch executor does not support the call")
	}
	var toolArguments arguments
	if err := json.Unmarshal(call.NormalizedArguments, &toolArguments); err != nil ||
		toolArguments.Patch == "" || toolArguments.Path != call.Path {
		return toolcontracts.ToolResult{}, errors.New("normalized apply_patch arguments are invalid")
	}
	operations, err := parsePatch(toolArguments.Patch)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if !authorizedDirectory(call.Path, call.WriteScopes) {
		return toolcontracts.ToolResult{}, errors.New("apply_patch root is outside an authorized directory")
	}
	changes, results, err := prepareChanges(ctx, call.Path, call.WriteScopes, operations)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	encoded, err := json.Marshal(output{Files: results})
	if err != nil || len(encoded) > toolmodel.MaxInlineToolResultBytes {
		return toolcontracts.ToolResult{}, errors.New("apply_patch result exceeds the size limit")
	}
	if err := ctx.Err(); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if err := commitChanges(changes); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	result := toolcontracts.NewToolSuccess(string(encoded), false)
	result.SideEffect = true
	return result, nil
}

func prepareChanges(
	ctx context.Context,
	root string,
	scopes []string,
	operations []operation,
) ([]*fileChange, []fileResult, error) {
	changes := make([]*fileChange, 0, len(operations)+1)
	results := make([]fileResult, 0, len(operations))
	seen := make(map[string]struct{}, len(operations)+1)
	for _, operation := range operations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		source, err := resolveTarget(root, operation.path)
		if err != nil || !rememberPath(seen, source) {
			return nil, nil, fmt.Errorf("patch target %q is duplicated or invalid", operation.path)
		}
		switch operation.kind {
		case operationAdd:
			if !authorizedWritePath(source, scopes, true) {
				return nil, nil, fmt.Errorf("add target %q is outside an authorized write scope", operation.path)
			}
			if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("add target %q already exists", operation.path)
			}
			changes = append(changes, &fileChange{
				path: source, content: addContent(operation.content), mode: 0o644,
			})
			results = append(results, fileResult{Path: operation.path, Action: "added"})
		case operationDelete:
			mode, err := statAuthorizedFile(source, scopes)
			if err != nil {
				return nil, nil, fmt.Errorf("delete target %q: %w", operation.path, err)
			}
			changes = append(changes, &fileChange{path: source, mode: mode, delete: true, existed: true})
			results = append(results, fileResult{Path: operation.path, Action: "deleted"})
		case operationUpdate:
			content, mode, err := readAuthorizedFile(source, scopes)
			if err != nil {
				return nil, nil, fmt.Errorf("update target %q: %w", operation.path, err)
			}
			updated, err := applyHunks(content, operation.hunks)
			if err != nil {
				return nil, nil, fmt.Errorf("update target %q: %w", operation.path, err)
			}
			if operation.moveTo == "" {
				changes = append(changes, &fileChange{path: source, content: updated, mode: mode, existed: true})
				results = append(results, fileResult{Path: operation.path, Action: "updated"})
				continue
			}
			destination, err := resolveTarget(root, operation.moveTo)
			if err != nil || !rememberPath(seen, destination) || !authorizedWritePath(destination, scopes, true) {
				return nil, nil, fmt.Errorf("move target %q is duplicated, invalid, or outside an authorized write scope", operation.moveTo)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("move target %q already exists", operation.moveTo)
			}
			changes = append(changes,
				&fileChange{path: source, mode: mode, delete: true, existed: true},
				&fileChange{path: destination, content: updated, mode: mode},
			)
			results = append(results, fileResult{Path: operation.moveTo, From: operation.path, Action: "moved"})
		default:
			return nil, nil, errors.New("patch contains an unknown file operation")
		}
	}
	return changes, results, nil
}

func addContent(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func resolveTarget(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", errors.New("patch target must be relative")
	}
	return filepath.Abs(filepath.Join(root, path))
}

func rememberPath(seen map[string]struct{}, path string) bool {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	if _, exists := seen[key]; exists {
		return false
	}
	seen[key] = struct{}{}
	return true
}

func readAuthorizedFile(path string, scopes []string) ([]byte, fs.FileMode, error) {
	mode, err := statAuthorizedFile(path, scopes)
	if err != nil {
		return nil, 0, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, errors.New("file could not be read")
	}
	if !utf8.Valid(content) {
		return nil, 0, errors.New("apply_patch supports UTF-8 text files only")
	}
	return content, mode, nil
}

func statAuthorizedFile(path string, scopes []string) (fs.FileMode, error) {
	if !authorizedWritePath(path, scopes, false) {
		return 0, errors.New("file is outside an authorized write scope or is not a regular file")
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, errors.New("file could not be read")
	}
	return info.Mode().Perm(), nil
}

func applyHunks(content []byte, hunks []hunk) ([]byte, error) {
	if !utf8.Valid(content) {
		return nil, errors.New("apply_patch supports UTF-8 text files only")
	}
	newline := "\n"
	if strings.Contains(string(content), "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	endsWithNewline := strings.HasSuffix(normalized, "\n")
	if endsWithNewline {
		normalized = strings.TrimSuffix(normalized, "\n")
	}
	lines := strings.Split(normalized, "\n")
	if normalized == "" && !endsWithNewline {
		lines = nil
	}
	cursor := 0
	for _, value := range hunks {
		index := findHunk(lines, value, cursor)
		if index < 0 {
			return nil, errors.New("hunk context does not match the file")
		}
		updated := make([]string, 0, len(lines)-len(value.oldLines)+len(value.newLines))
		updated = append(updated, lines[:index]...)
		updated = append(updated, value.newLines...)
		updated = append(updated, lines[index+len(value.oldLines):]...)
		lines = updated
		cursor = index + len(value.newLines)
	}
	result := strings.Join(lines, "\n")
	if endsWithNewline && len(lines) > 0 {
		result += "\n"
	}
	if newline != "\n" {
		result = strings.ReplaceAll(result, "\n", newline)
	}
	return []byte(result), nil
}

func findHunk(lines []string, value hunk, cursor int) int {
	if len(value.oldLines) == 0 {
		if value.oldStart > 0 {
			return min(value.oldStart-1, len(lines))
		}
		return min(cursor, len(lines))
	}
	preferred := cursor
	if value.oldStart > 0 {
		preferred = value.oldStart - 1
	}
	if hunkMatches(lines, preferred, value.oldLines) {
		return preferred
	}
	for index := max(0, cursor); index+len(value.oldLines) <= len(lines); index++ {
		if hunkMatches(lines, index, value.oldLines) {
			return index
		}
	}
	return -1
}

func hunkMatches(lines []string, index int, expected []string) bool {
	if index < 0 || index+len(expected) > len(lines) {
		return false
	}
	for offset, line := range expected {
		if lines[index+offset] != line {
			return false
		}
	}
	return true
}

func commitChanges(changes []*fileChange) error {
	cleanup := func() {
		for _, change := range changes {
			if change.temporary != "" {
				_ = os.Remove(change.temporary)
			}
			if change.backup != "" {
				_ = os.Remove(change.backup)
			}
		}
	}
	for _, change := range changes {
		if change.delete {
			continue
		}
		temporary, err := os.CreateTemp(filepath.Dir(change.path), ".praxis-apply-patch-")
		if err != nil {
			cleanup()
			return errors.New("apply_patch could not create a temporary file")
		}
		change.temporary = temporary.Name()
		if err := temporary.Chmod(change.mode); err != nil {
			_ = temporary.Close()
			cleanup()
			return errors.New("apply_patch could not stage a file")
		}
		if _, err := temporary.Write(change.content); err != nil {
			_ = temporary.Close()
			cleanup()
			return errors.New("apply_patch could not stage a file")
		}
		if err := temporary.Close(); err != nil {
			cleanup()
			return errors.New("apply_patch could not stage a file")
		}
	}

	rollback := func() {
		for index := len(changes) - 1; index >= 0; index-- {
			change := changes[index]
			if change.installed {
				_ = os.Remove(change.path)
			}
			if change.backup != "" {
				_ = os.Rename(change.backup, change.path)
			}
		}
		cleanup()
	}
	for _, change := range changes {
		if !change.existed {
			continue
		}
		backup, err := os.CreateTemp(filepath.Dir(change.path), ".praxis-apply-patch-backup-")
		if err != nil {
			rollback()
			return errors.New("apply_patch could not prepare the original file")
		}
		change.backup = backup.Name()
		if err := backup.Close(); err != nil {
			_ = os.Remove(change.backup)
			change.backup = ""
			rollback()
			return errors.New("apply_patch could not prepare the original file")
		}
		if err := os.Remove(change.backup); err != nil || os.Rename(change.path, change.backup) != nil {
			_ = os.Remove(change.backup)
			change.backup = ""
			rollback()
			return errors.New("apply_patch could not preserve the original file")
		}
	}
	for _, change := range changes {
		if change.delete {
			continue
		}
		if err := os.Rename(change.temporary, change.path); err != nil {
			rollback()
			return errors.New("apply_patch could not install the updated file")
		}
		change.temporary = ""
		change.installed = true
	}
	cleanup()
	return nil
}

func authorizedDirectory(path string, scopes []string) bool {
	cleanPath := filepath.Clean(path)
	info, err := os.Lstat(cleanPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil || !pathutil.SamePath(cleanPath, realPath) {
		return false
	}
	for _, scope := range scopes {
		cleanScope := filepath.Clean(scope)
		if !pathutil.IsWithin(cleanScope, cleanPath) {
			continue
		}
		realScope, err := filepath.EvalSymlinks(cleanScope)
		if err == nil && pathutil.SamePath(cleanScope, realScope) && pathutil.IsWithin(realScope, realPath) {
			return true
		}
	}
	return false
}

func authorizedWritePath(path string, scopes []string, allowMissing bool) bool {
	cleanPath := filepath.Clean(path)
	for _, scope := range scopes {
		cleanScope := filepath.Clean(scope)
		if !pathutil.IsWithin(cleanScope, cleanPath) {
			continue
		}
		realScope, err := filepath.EvalSymlinks(cleanScope)
		if err != nil || !pathutil.SamePath(cleanScope, realScope) {
			continue
		}
		info, err := os.Lstat(cleanPath)
		if err == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
			realPath, err := filepath.EvalSymlinks(cleanPath)
			return err == nil && pathutil.SamePath(cleanPath, realPath) && pathutil.IsWithin(realScope, realPath)
		}
		if !allowMissing || !errors.Is(err, os.ErrNotExist) {
			continue
		}
		parent := filepath.Dir(cleanPath)
		parentInfo, parentErr := os.Stat(parent)
		realParent, evalErr := filepath.EvalSymlinks(parent)
		if parentErr == nil && parentInfo.IsDir() && evalErr == nil && pathutil.SamePath(parent, realParent) && pathutil.IsWithin(realScope, realParent) {
			return true
		}
	}
	return false
}
