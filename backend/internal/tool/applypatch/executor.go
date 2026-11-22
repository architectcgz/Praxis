package applypatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"praxis/internal/pathutil"
	"praxis/internal/tool"
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

// call 在项目目录内应用已在 Prepare 阶段解析的完整补丁。
func call(ctx context.Context, toolArguments arguments, environment tool.Environment) (tool.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	if !projectDirectory(toolArguments.Path, environment.ProjectPath) {
		return tool.ToolResult{}, errors.New("apply_patch root is outside the project")
	}
	changes, results, err := prepareChanges(ctx, toolArguments.Path, environment.ProjectPath, toolArguments.operations)
	if err != nil {
		return tool.ToolResult{}, err
	}
	encoded, err := json.Marshal(output{Files: results})
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("encode apply_patch result: %w", err)
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
	projectRoot string,
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
		case addFile:
			if !projectWritePath(source, projectRoot, true) {
				return nil, nil, fmt.Errorf("add target %q is outside the project", operation.path)
			}
			if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("add target %q already exists", operation.path)
			}
			content, err := operation.apply(nil)
			if err != nil {
				return nil, nil, err
			}
			changes = append(changes, &fileChange{
				path:    source,
				content: content,
				mode:    0o644,
			})
			results = append(results, fileResult{Path: operation.path, Action: "added"})
		case deleteFile:
			mode, err := statProjectFile(source, projectRoot)
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
		case updateFile:
			content, mode, err := readProjectFile(source, projectRoot)
			if err != nil {
				return nil, nil, fmt.Errorf("update target %q: %w", operation.path, err)
			}
			updated, err := operation.apply(content)
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
			if err != nil || !rememberPath(seen, destination) || !projectWritePath(destination, projectRoot, true) {
				return nil, nil, fmt.Errorf("move target %q is duplicated, invalid, or outside the project", operation.moveTo)
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

func readProjectFile(path string, projectRoot string) ([]byte, fs.FileMode, error) {
	mode, err := statProjectFile(path, projectRoot)
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

func statProjectFile(path string, projectRoot string) (fs.FileMode, error) {
	if !projectWritePath(path, projectRoot, false) {
		return 0, errors.New("file is outside the project or is not a regular file")
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, errors.New("file could not be read")
	}
	return info.Mode().Perm(), nil
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

func projectDirectory(path, projectRoot string) bool {
	if !pathutil.IsWithin(projectRoot, path) {
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
	realRoot, err := filepath.EvalSymlinks(projectRoot)
	return err == nil && pathutil.SamePath(projectRoot, realRoot) && pathutil.IsWithin(realRoot, realPath)
}

func projectWritePath(path, projectRoot string, allowMissing bool) bool {
	if !pathutil.IsAbsoluteNormalized(path) || !pathutil.IsWithin(projectRoot, path) {
		return false
	}
	realRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil || !pathutil.SamePath(projectRoot, realRoot) {
		return false
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		realPath, err := filepath.EvalSymlinks(path)
		return err == nil && pathutil.SamePath(path, realPath) && pathutil.IsWithin(realRoot, realPath)
	}
	if !allowMissing || !errors.Is(err, os.ErrNotExist) {
		return false
	}
	parent := filepath.Dir(path)
	parentInfo, parentErr := os.Stat(parent)
	realParent, evalErr := filepath.EvalSymlinks(parent)
	return parentErr == nil && parentInfo.IsDir() && evalErr == nil && pathutil.SamePath(parent, realParent) && pathutil.IsWithin(realRoot, realParent)
}
