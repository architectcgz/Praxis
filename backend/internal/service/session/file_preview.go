package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"praxis/internal/contracts"
)

const maxPreviewBytes = 1 << 20

// FilePreview 是工作区内文本文件的只读快照；Path 为工作区相对路径，不暴露任意磁盘目录。
type FilePreview struct {
	Path    string
	Content string
}

// PreviewFile 按会话定位 Workspace 并读取指定文件；拒绝越界、非普通文件、非 UTF-8 文本和超过 1 MiB 的文件。
// 输入路径仅在此入口规范化，根目录由持久化的会话关系确定，不能由前端指定。
func (s *Service) PreviewFile(ctx context.Context, sessionID contracts.SessionID, path string) (FilePreview, error) {
	if ctx == nil || sessionID == "" {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "文件预览需要有效会话。")
	}
	if err := ctx.Err(); err != nil {
		return FilePreview{}, err
	}
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsRune(path, '\x00') || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "文件路径无效。")
	}
	// Git Bash 会把 Windows 盘符路径表示为 /c/...；先转换后再与工作区路径比较。
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == '/' &&
		((path[1] >= 'a' && path[1] <= 'z') || (path[1] >= 'A' && path[1] <= 'Z')) {
		path = path[1:2] + ":" + path[2:]
	}
	workspace, err := s.workspaceForSession(ctx, sessionID)
	if err != nil {
		return FilePreview{}, err
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(workspace.Path, path)
		if err != nil {
			return FilePreview{}, contracts.New(contracts.InvalidRequest, "只能预览当前工作区内的文件。")
		}
	}
	// IsLocal 拦截词法越界；冒号额外拒绝 Windows 驱动器相对路径与 NTFS 数据流。
	if !filepath.IsLocal(path) || path == "." || strings.ContainsRune(path, ':') {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "只能预览当前工作区内的文件。")
	}
	return readPreviewFile(ctx, workspace.Path, path)
}

func readPreviewFile(ctx context.Context, directory, path string) (FilePreview, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return FilePreview{}, fmt.Errorf("无法打开工作区：%w", err)
	}
	defer root.Close()
	// Root 限制符号链接的解析范围，避免检查路径后再打开造成的目录逃逸。
	info, err := root.Stat(path)
	if err != nil {
		return FilePreview{}, fmt.Errorf("无法预览文件：%w", err)
	}
	if !info.Mode().IsRegular() {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "只能预览普通文本文件。")
	}
	if info.Size() > maxPreviewBytes {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "文件超过 1 MiB，无法预览。")
	}
	file, err := root.Open(path)
	if err != nil {
		return FilePreview{}, fmt.Errorf("无法读取文件：%w", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return FilePreview{}, err
	}
	if !info.Mode().IsRegular() {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "只能预览普通文本文件。")
	}
	// 文件可能仍在写入，读取也必须限量，不能仅信任打开前的大小。
	data, err := io.ReadAll(io.LimitReader(file, maxPreviewBytes+1))
	if err != nil {
		return FilePreview{}, fmt.Errorf("无法读取文件：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return FilePreview{}, err
	}
	if len(data) > maxPreviewBytes {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "文件超过 1 MiB，无法预览。")
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "仅支持 UTF-8 文本预览，不支持二进制文件。")
	}
	return FilePreview{
		Path:    filepath.ToSlash(path),
		Content: strings.TrimPrefix(string(data), "\uFEFF"),
	}, nil
}
