package arguments

import (
	"errors"
	"path/filepath"
	"praxis/internal/utils/pathutil"
)

// ResolvePath 在工具参数边界相对工作区解析路径，权限校验仍由执行前检查完成。
func ResolvePath(path, workspacePath string) (string, error) {
	if !pathutil.IsAbsoluteNormalized(workspacePath) {
		return "", errors.New("tool workspace path must be canonical")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspacePath, path)
	}
	return filepath.Clean(path), nil
}
