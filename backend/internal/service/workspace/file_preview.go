package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	workspaceio "praxis/internal/workspace"
	"strings"
)

const maxPreviewBytes int64 = 1 << 20

// FilePreview 是工作区内文本文件的只读快照；Path 为工作区相对路径。
type FilePreview struct {
	Path    string
	Content string
}

// PreviewFile 接收 canonical 会话身份和路径，在工作区授权后读取有界文本。
func (s *Service) PreviewFile(ctx context.Context, sessionID identity.SessionID, path string) (FilePreview, error) {
	if ctx == nil {
		return FilePreview{}, fmt.Errorf("a valid session is required to preview files: %w", errorcode.ErrInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return FilePreview{}, err
	}
	workspace, err := s.workspaceForSession(ctx, sessionID)
	if err != nil {
		return FilePreview{}, err
	}
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(workspace.Path, path)
		if err != nil {
			return FilePreview{}, fmt.Errorf("only files within the current workspace can be previewed: %w", errorcode.ErrInvalidRequest)
		}
	}
	// IsLocal 拦截词法越界；冒号额外拒绝 Windows 驱动器相对路径与 NTFS 数据流。
	if !filepath.IsLocal(path) || path == "." || strings.ContainsRune(path, ':') {
		return FilePreview{}, fmt.Errorf("only files within the current workspace can be previewed: %w", errorcode.ErrInvalidRequest)
	}
	content, err := s.textReader.ReadText(ctx, workspace.Path, path, maxPreviewBytes)
	if errors.Is(err, workspaceio.ErrFileTooLarge) {
		return FilePreview{}, fmt.Errorf("file exceeds the 1 MiB preview limit: %w", errorcode.ErrInvalidRequest)
	}
	if errors.Is(err, workspaceio.ErrNotRegularFile) {
		return FilePreview{}, fmt.Errorf("only regular text files can be previewed: %w", errorcode.ErrInvalidRequest)
	}
	if errors.Is(err, workspaceio.ErrInvalidText) {
		return FilePreview{}, fmt.Errorf("only UTF-8 text can be previewed; binary files are not supported: %w", errorcode.ErrInvalidRequest)
	}
	if err != nil {
		return FilePreview{}, err
	}
	return FilePreview{
		Path:    filepath.ToSlash(path),
		Content: content,
	}, nil
}
