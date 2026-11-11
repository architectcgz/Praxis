// Package jsonl 提供不解释业务载荷的逐行日志文件读写。
package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Position 定位一条包含换行符的完整记录，不承载业务身份。
type Position struct {
	Offset int64
	Length int64
}

func CheckRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("log must be a regular file")
	}
	return nil
}

// Create 建立空日志，不覆盖已有文件；成功返回不保证断电持久化。
func Create(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create log %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("create log %s: %w", path, err)
	}
	return nil
}

// Append 不补建文件、不回滚失败写入、不逐条同步；调用方负责串行追加及失败后的处理。
func Append(path string, data []byte) (Position, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Position{}, err
	}
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return Position{}, errors.Join(err, file.Close())
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return Position{}, err
	}
	return Position{Offset: offset, Length: int64(n)}, nil
}

// ReadFrom 从连续索引位置恢复读取；未换行的尾部备份后截断，调用方须排除并发读写。
func ReadFrom(ctx context.Context, path, backupDir string, offset int64, apply func(Position, []byte) error) error {
	return readFrom(ctx, path, backupDir, offset, true, apply)
}

// ScanFrom 只交付完整记录；普通索引补齐不备份或截断尚未完成的尾行。
func ScanFrom(ctx context.Context, path string, offset int64, apply func(Position, []byte) error) error {
	return readFrom(ctx, path, "", offset, false, apply)
}

func readFrom(ctx context.Context, path, backupDir string, offset int64, recoverTail bool, apply func(Position, []byte) error) (err error) {
	flags := os.O_RDONLY
	if recoverTail { flags = os.O_RDWR }
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	if err := checkBoundary(file, offset); err != nil {
		return err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			if len(line) > 0 && recoverTail {
				if err := truncateTail(file, backupDir, offset, line); err != nil {
					return fmt.Errorf("%s offset %d: %w", path, offset, err)
				}
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if err := apply(Position{Offset: offset, Length: int64(len(line))}, line); err != nil {
			return fmt.Errorf("%s offset %d: %w", path, offset, err)
		}
		offset += int64(len(line))
	}
}

// ReadAt 只读已完整写入的一行，不执行尾行修复或读取其他记录。
func ReadAt(ctx context.Context, path string, position Position) (data []byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if position.Length <= 0 {
		return nil, errors.New("log record length must be positive")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := checkBoundary(file, position.Offset); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if position.Offset > info.Size() || position.Length > info.Size()-position.Offset {
		return nil, fmt.Errorf("%s offset %d: log record exceeds file size", path, position.Offset)
	}
	data = make([]byte, position.Length)
	if _, err := file.ReadAt(data, position.Offset); err != nil {
		return nil, fmt.Errorf("%s offset %d: %w", path, position.Offset, err)
	}
	if data[len(data)-1] != '\n' || bytes.IndexByte(data[:len(data)-1], '\n') >= 0 {
		return nil, fmt.Errorf("%s offset %d: expected one complete log record", path, position.Offset)
	}
	return data, nil
}

func checkBoundary(file *os.File, offset int64) error {
	if offset < 0 {
		return errors.New("log offset must not be negative")
	}
	if offset == 0 {
		return nil
	}
	var previous [1]byte
	if _, err := file.ReadAt(previous[:], offset-1); err != nil {
		return fmt.Errorf("log offset %d: %w", offset, err)
	}
	if previous[0] != '\n' {
		return fmt.Errorf("log offset %d is not a complete record boundary", offset)
	}
	return nil
}

// truncateTail 先同步尾行备份及其目录，再截断原日志，避免修复时丢失待检查的原始内容。
func truncateTail(file *os.File, backupDir string, offset int64, tail []byte) error {
	backup, err := os.CreateTemp(backupDir, "incomplete-tail-*")
	if err != nil {
		return err
	}
	_, writeErr := backup.Write(tail)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		return err
	}
	if err := syncDirectory(backupDir); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}
