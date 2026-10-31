package project

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/projectfile"
	"strings"
)

const maxPreviewBytes int64 = 1 << 20

// FilePreview 是项目目录内文本文件的只读快照；Path 为项目相对路径。
type FilePreview struct {
	Path    string
	Content string
}

// PreviewFile 接收已校验的会话身份和路径，按会话所属项目限制文件读取范围。
func (s *Service) PreviewFile(ctx context.Context, sessionID identity.SessionID, path string) (FilePreview, error) {
	if err := ctx.Err(); err != nil {
		return FilePreview{}, err
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return FilePreview{}, err
	}
	project, err := s.projects.Get(ctx, session.ProjectID)
	if err != nil {
		return FilePreview{}, err
	}
	if project.State != projectmodel.ProjectActive {
		return FilePreview{}, ErrProjectInvalid
	}
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(project.Path, path)
		if err != nil {
			return FilePreview{}, fmt.Errorf("only files within the current project can be previewed: %w", errorcode.ErrInvalidRequest)
		}
	}
	// IsLocal 拦截词法越界；冒号额外拒绝 Windows 驱动器相对路径与 NTFS 数据流。
	if !filepath.IsLocal(path) || path == "." || strings.ContainsRune(path, ':') {
		return FilePreview{}, fmt.Errorf("only files within the current project can be previewed: %w", errorcode.ErrInvalidRequest)
	}
	content, err := s.textReader.ReadText(ctx, project.Path, path, maxPreviewBytes)
	if errors.Is(err, projectfile.ErrFileTooLarge) {
		return FilePreview{}, fmt.Errorf("file exceeds the 1 MiB preview limit: %w", errorcode.ErrInvalidRequest)
	}
	if errors.Is(err, projectfile.ErrNotRegularFile) {
		return FilePreview{}, fmt.Errorf("only regular text files can be previewed: %w", errorcode.ErrInvalidRequest)
	}
	if errors.Is(err, projectfile.ErrInvalidText) {
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
