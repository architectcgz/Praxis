package bindings

import (
	"praxis/internal/contracts"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// PreviewFile 预览会话工作区内的文本文件；业务层负责路径范围、类型和大小限制，失败返回可读错误。
func (b *SessionBindings) PreviewFile(sessionID, path string) (dto.FilePreview, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.FilePreview{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.FilePreview{}, err
	}
	preview, err := service.Sessions.PreviewFile(ctx, contracts.SessionID(sessionID), path)
	if err != nil {
		return dto.FilePreview{}, publicError(b.runtime, "SessionBindings.PreviewFile", err)
	}
	return dto.FilePreview{
		Path:    preview.Path,
		Content: preview.Content,
	}, nil
}
