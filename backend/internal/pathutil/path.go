package pathutil

import (
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizeAbsolutePath 清理路径两端空白并执行平台相关的路径规范化。
// 是否为绝对路径由调用方根据具体业务规则校验。
func NormalizeAbsolutePath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

// IsAbsoluteNormalized 判断路径是否为绝对、已规范化且不含控制字符的路径。
func IsAbsoluteNormalized(path string) bool {
	return path != "" && path != "." && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

// SamePath 按当前操作系统规则比较两个规范化后的路径。
func SamePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// IsWithin 判断 candidate 是否位于 root 目录内，root 本身也算在内。
func IsWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
