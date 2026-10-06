package bindings

import (
	"praxis/internal/contracts"
	"praxis/wails/validation"
)

// RevealSessionFile 打开系统文件管理器定位会话日志；非法 ID、文件缺失或启动失败时返回公开错误。
func (b *SessionBindings) RevealSessionFile(sessionID string) error {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	path, err := services.SessionLogPath(ctx, contracts.SessionID(sessionID))
	if err != nil {
		return publicError(b.runtime, "SessionBindings.RevealSessionFile", err)
	}
	command, err := revealFileCommand(path)
	if err != nil {
		return publicError(b.runtime, "SessionBindings.RevealSessionFile", err)
	}
	if err := command.Start(); err != nil {
		return publicError(b.runtime, "SessionBindings.RevealSessionFile", err)
	}
	// 文件管理器可能持续运行，不阻塞 binding；后台等待以释放子进程资源。
	go func() {
		if err := command.Wait(); err != nil {
			b.runtime.LogError("session file manager exited: %v", err)
		}
	}()
	return nil
}
