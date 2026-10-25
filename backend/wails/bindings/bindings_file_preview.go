package bindings

import (
	"path/filepath"
	"runtime"
	"strings"

	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// PreviewFile 预览会话工作区内的文本文件；业务层负责路径范围、类型和大小限制，失败返回可读错误。
func (b *SessionBindings) PreviewFile(sessionID, path string) (dto.FilePreview, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.FilePreview{}, publicInputError(b.host, "SessionBindings.PreviewFile.request", err)
	}
	path = strings.TrimSpace(path)
	if err := validation.Value("path", path, "required,preview_path"); err != nil {
		return dto.FilePreview{}, publicInputError(b.host, "SessionBindings.PreviewFile.request", err)
	}
	ctx, service, err := b.host.BindingContext()
	if err != nil {
		return dto.FilePreview{}, err
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == '/' &&
		((path[1] >= 'a' && path[1] <= 'z') || (path[1] >= 'A' && path[1] <= 'Z')) {
		path = path[1:2] + ":" + path[2:]
	}
	path = filepath.Clean(filepath.FromSlash(path))
	preview, err := service.Workspaces.PreviewFile(ctx, identity.SessionID(sessionID), path)
	if err != nil {
		return dto.FilePreview{}, publicError(b.host, "SessionBindings.PreviewFile", err)
	}
	return dto.FilePreview{
		Path:    preview.Path,
		Content: preview.Content,
	}, nil
}
