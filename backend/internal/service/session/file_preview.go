package session

import (
	"context"
	"path/filepath"
	"strings"

	"praxis/internal/contracts"
	"praxis/internal/request"
)

const maxPreviewBytes int64 = 1 << 20

// FilePreview 是工作区内文本文件的只读快照；Path 为工作区相对路径。
type FilePreview struct {
	Path    string
	Content string
}

// PreviewFile 通过持久化会话解析 Workspace，并在授权后委托 Port 读取文本。
func (s *Service) PreviewFile(ctx context.Context, canonical request.FilePreview) (FilePreview, error) {
	if ctx == nil {
		return FilePreview{}, contracts.New(contracts.InvalidRequest, "文件预览需要有效会话。")
	}
	if err := ctx.Err(); err != nil {
		return FilePreview{}, err
	}
	workspace, err := s.workspaceForSession(ctx, canonical.SessionID)
	if err != nil {
		return FilePreview{}, err
	}
	path := canonical.Path
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
	content, err := s.textReader.ReadText(ctx, workspace.Path, path, maxPreviewBytes)
	if err != nil {
		return FilePreview{}, err
	}
	return FilePreview{
		Path:    filepath.ToSlash(path),
		Content: content,
	}, nil
}
