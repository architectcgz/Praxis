// Package logging 为桌面应用提供进程级诊断日志输出。
// 日志写入 stderr，并在配置后同时写入受保护的运行日志文件。
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Level 控制输出的日志级别，默认为 InfoLevel，
// 以记录生命周期和命令边界，无需记录每次查询。
type Level uint8

const (
	DebugLevel Level = iota + 1
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Factory 统一管理日志输出目标和环境变量配置。
// 调用方应依赖返回的 Logger，而不是自行组装输出 Writer。
type Factory struct {
	level Level
}

// NewFactory 创建日志工厂，并在 PRAXIS_LOG_LEVEL 有效时采用该配置。
func NewFactory() Factory {
	result := Factory{level: InfoLevel}
	if level, ok := ParseLevel(os.Getenv("PRAXIS_LOG_LEVEL")); ok {
		result.level = level
	}
	return result
}

// Logger 串行化写入，防止并发运行时交错写入日志，
// 或在 Wails 关闭文件时继续写入。
type Logger struct {
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
	level  Level
	closed bool
}

// newLogger 为工厂选定的输出目标创建日志器。
func newLogger(writer io.Writer) *Logger {
	if writer == nil {
		writer = os.Stderr
	}
	return &Logger{
		logger: log.New(writer, "", log.LstdFlags|log.Lmicroseconds),
		level:  InfoLevel,
	}
}

// newNop 为组件装配前的构造路径创建禁用的日志输出。
func newNop() *Logger {
	return &Logger{logger: log.New(io.Discard, "", 0), level: ErrorLevel + 1}
}

// Nop 为组件装配前的构造路径返回禁用的日志器。
func (f Factory) Nop() *Logger { return newNop() }

// Ensure 在注入边界将 nil 日志器替换为禁用的日志输出。
func (f Factory) Ensure(logger *Logger) *Logger {
	if logger == nil {
		return f.Nop()
	}
	return logger
}

// Console 返回写入当前进程 stderr 的日志器。
func (f Factory) Console() *Logger {
	result := newLogger(os.Stderr)
	result.SetLevel(f.level)
	return result
}

// Application 返回同时写入 stderr 和受保护文件的日志器。
func (f Factory) Application(path string) (*Logger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	// GUI 进程可能没有可用的 stderr，先落盘，避免控制台写入失败截断文件日志。
	result := newLogger(io.MultiWriter(file, os.Stderr))
	result.file = file
	result.SetLevel(f.level)
	return result, nil
}

// SetLevel 修改后续日志输出的最低级别。
func (l *Logger) SetLevel(level Level) {
	if l == nil || level < DebugLevel || level > ErrorLevel {
		return
	}
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// ParseLevel 解析 debug、info、warn/warning 或 error 日志级别。
func ParseLevel(value string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return DebugLevel, true
	case "info":
		return InfoLevel, true
	case "warn", "warning":
		return WarnLevel, true
	case "error":
		return ErrorLevel, true
	default:
		return 0, false
	}
}

// Debugf 记录诊断命令或 Provider 调用所需的详细信息。
func (l *Logger) Debugf(format string, args ...any) { l.logf(DebugLevel, "DEBUG", format, args...) }

// Infof 记录正常的生命周期和命令事件。
func (l *Logger) Infof(format string, args ...any) { l.logf(InfoLevel, "INFO", format, args...) }

// Warnf 记录可恢复的异常情况。
func (l *Logger) Warnf(format string, args ...any) { l.logf(WarnLevel, "WARN", format, args...) }

// Errorf 记录需要关注的失败。
func (l *Logger) Errorf(format string, args ...any) { l.logf(ErrorLevel, "ERROR", format, args...) }

func (l *Logger) logf(level Level, label, format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || level < l.level {
		return
	}
	l.logger.Printf("[%s] %s", label, formatMessage(format, args...))
}

func formatMessage(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// Close 刷新并关闭可选的运行日志文件。
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
