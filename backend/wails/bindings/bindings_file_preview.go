package bindings

import (
	"path/filepath"
	"runtime"
	"strings"

	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// PreviewFile 预览会话工作区内的文本文件；业务层负责路径范围、类型和大小限制，失败返回可读错误。
func (b *SessionBindings) PreviewFile(sessionID, path string) (dto.FilePreview, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.FilePreview{}, err
	}
	ctx, service, err := b.host.BindingContext()
	if err != nil {
		return dto.FilePreview{}, err
	}
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsRune(path, '\x00') || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		err := fault.InvalidValue("path", "file path is invalid")
		return dto.FilePreview{}, publicError(b.host, "SessionBindings.PreviewFile.request", err)
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == '/' &&
		((path[1] >= 'a' && path[1] <= 'z') || (path[1] >= 'A' && path[1] <= 'Z')) {
		path = path[1:2] + ":" + path[2:]
	}
	path = filepath.Clean(filepath.FromSlash(path))
	preview, err := service.Sessions.PreviewFile(ctx, identity.SessionID(sessionID), path)
	if err != nil {
		return dto.FilePreview{}, publicError(b.host, "SessionBindings.PreviewFile", err)
	}
	return dto.FilePreview{
		Path:    preview.Path,
		Content: preview.Content,
	}, nil
}
