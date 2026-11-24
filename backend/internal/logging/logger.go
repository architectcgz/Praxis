// Package logging 为桌面应用提供进程级诊断日志输出。
// 输出目标由启动边界注入，运行日志可同时写入受保护的文件。
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
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

// Logger 串行化写入，防止并发运行时交错写入日志，
// 或在 Wails 关闭文件时继续写入。
type Logger struct {
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
	level  Level
	closed bool
}

func New(writer io.Writer, level Level) *Logger {
	if writer == nil {
		writer = io.Discard
	}
	return &Logger{
		logger: log.New(writer, "", log.LstdFlags|log.Lmicroseconds),
		level:  level,
	}
}

// Open 写入受保护的文件；如果注入了外部输出目标，也写入该目标。
func Open(path string, writer io.Writer, level Level) (*Logger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	var output io.Writer = file
	if writer != nil {
		// 先落盘，避免 GUI 进程的控制台写入失败截断文件日志。
		output = io.MultiWriter(file, writer)
	}
	result := New(output, level)
	result.file = file
	return result, nil
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
	if l == nil || level < l.level {
		return
	}
	message := "[" + label + "] " + formatMessage(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.logger.Print(message)
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
