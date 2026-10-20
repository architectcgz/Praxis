package applypatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"praxis/internal/core/tool"
	"praxis/internal/utils/pathutil"
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

// Call 在 Workspace 内校验并应用完整补丁。
func Call(ctx context.Context, args json.RawMessage, workspacePath string) (tool.ToolResult, error) {
	if ctx == nil {
		return tool.ToolResult{}, errors.New("tool task context is required")
	}
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	var toolArguments arguments
	if err := json.Unmarshal(args, &toolArguments); err != nil ||
		toolArguments.Patch == "" {
		return tool.ToolResult{}, errors.New("apply_patch arguments are invalid")
	}
	operations, err := parsePatch(toolArguments.Patch)
	if err != nil {
		return tool.ToolResult{}, err
	}
	if !workspaceDirectory(toolArguments.Path, workspacePath) {
		return tool.ToolResult{}, errors.New("apply_patch root is outside the workspace")
	}
	changes, results, err := prepareChanges(ctx, toolArguments.Path, workspacePath, operations)
	if err != nil {
		return tool.ToolResult{}, err
	}
	encoded, err := json.Marshal(output{Files: results})
	if err != nil || len(encoded) > tool.MaxInlineResultBytes {
		return tool.ToolResult{}, errors.New("apply_patch result exceeds the size limit")
	}
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	if err := commitChanges(changes); err != nil {
		return tool.ToolResult{}, err
	}
	result := tool.NewToolSuccess(string(encoded), false)
	result.SideEffect = true
	return result, nil
}

func prepareChanges(
	ctx context.Context,
	root string,
	workspace string,
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
			if !workspaceWritePath(source, workspace, true) {
				return nil, nil, fmt.Errorf("add target %q is outside the workspace", operation.path)
			}
			if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("add target %q already exists", operation.path)
			}
			changes = append(changes, &fileChange{
				path: source, content: addContent(operation.content), mode: 0o644,
			})
			results = append(results, fileResult{Path: operation.path, Action: "added"})
		case operationDelete:
			mode, err := statWorkspaceFile(source, workspace)
			if err != nil {
				return nil, nil, fmt.Errorf("delete target %q: %w", operation.path, err)
			}
			changes = append(changes, &fileChange{
				path:    source,
				mode:    mode,
				delete:  true,
				existed: true,
			})
			results = append(results, fileResult{Path: operation.path, Action: "deleted"})
		case operationUpdate:
			content, mode, err := readWorkspaceFile(source, workspace)
			if err != nil {
				return nil, nil, fmt.Errorf("update target %q: %w", operation.path, err)
			}
			updated, err := applyHunks(content, operation.hunks)
			if err != nil {
				return nil, nil, fmt.Errorf("update target %q: %w", operation.path, err)
			}
			if operation.moveTo == "" {
				changes = append(changes, &fileChange{
					path:    source,
					content: updated,
					mode:    mode,
					existed: true,
				})
				results = append(results, fileResult{Path: operation.path, Action: "updated"})
				continue
			}
			destination, err := resolveTarget(root, operation.moveTo)
			if err != nil || !rememberPath(seen, destination) || !workspaceWritePath(destination, workspace, true) {
				return nil, nil, fmt.Errorf("move target %q is duplicated, invalid, or outside the workspace", operation.moveTo)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("move target %q already exists", operation.moveTo)
			}
			changes = append(changes,
				&fileChange{
					path:    source,
					mode:    mode,
					delete:  true,
					existed: true,
				},
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

func readWorkspaceFile(path string, workspace string) ([]byte, fs.FileMode, error) {
	mode, err := statWorkspaceFile(path, workspace)
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

func statWorkspaceFile(path string, workspace string) (fs.FileMode, error) {
	if !workspaceWritePath(path, workspace, false) {
		return 0, errors.New("file is outside the workspace or is not a regular file")
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

func workspaceDirectory(path, workspace string) bool {
	if !pathutil.IsAbsoluteNormalized(path) || !pathutil.IsAbsoluteNormalized(workspace) || !pathutil.IsWithin(workspace, path) {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil || !pathutil.SamePath(path, realPath) {
		return false
	}
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	return err == nil && pathutil.SamePath(workspace, realWorkspace) && pathutil.IsWithin(realWorkspace, realPath)
}

func workspaceWritePath(path, workspace string, allowMissing bool) bool {
	if !pathutil.IsAbsoluteNormalized(path) || !pathutil.IsAbsoluteNormalized(workspace) || !pathutil.IsWithin(workspace, path) {
		return false
	}
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil || !pathutil.SamePath(workspace, realWorkspace) {
		return false
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		realPath, err := filepath.EvalSymlinks(path)
		return err == nil && pathutil.SamePath(path, realPath) && pathutil.IsWithin(realWorkspace, realPath)
	}
	if !allowMissing || !errors.Is(err, os.ErrNotExist) {
		return false
	}
	parent := filepath.Dir(path)
	parentInfo, parentErr := os.Stat(parent)
	realParent, evalErr := filepath.EvalSymlinks(parent)
	return parentErr == nil && parentInfo.IsDir() && evalErr == nil && pathutil.SamePath(parent, realParent) && pathutil.IsWithin(realWorkspace, realParent)
}
