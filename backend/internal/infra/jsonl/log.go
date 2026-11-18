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
	"slices"
	"sync"
)

const reverseScanBlockSize = 64 * 1024

// Position 定位一条包含换行符的完整记录，不承载业务身份。
type Position struct {
	Offset int64
	Length int64
}

// Record 是反向扫描得到的一条完整物理记录。
type Record struct {
	Position
	Data []byte
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

type commandKind uint8

const (
	appendCommand commandKind = iota
	syncCommand
	commitCommand
	readCommand
	closeCommand
)

type command struct {
	ctx    context.Context
	kind   commandKind
	data   []byte
	cursor *int64
	limit  int
	result chan commandResult
}

type commandResult struct {
	position   Position
	records    []Record
	nextCursor *int64
	hasMore    bool
	err        error
}

// Writer 将同一文件的 Append、Sync 和 Close 串行化到一个 goroutine。
type Writer struct {
	requests chan command
	closeMu  sync.Mutex
	closed   bool
	closeErr error
}

// OpenWriter 打开已经创建的 JSONL 文件并启动专属 Writer。
func OpenWriter(ctx context.Context, path string) (*Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	writer := &Writer{requests: make(chan command)}
	go writer.run(file)
	return writer, nil
}

func (w *Writer) run(file *os.File) {
	var fatalErr error
	for request := range w.requests {
		if request.kind != closeCommand && fatalErr != nil {
			request.result <- commandResult{err: fatalErr}
			continue
		}

		switch request.kind {
		case appendCommand:
			position, err := appendRecord(file, request.data)
			request.result <- commandResult{position: position, err: err}
		case syncCommand:
			err := file.Sync()
			if err != nil {
				fatalErr = fmt.Errorf("sync log: %w", err)
			}
			request.result <- commandResult{err: err}
		case commitCommand:
			position, err := appendRecord(file, request.data)
			if err == nil {
				err = file.Sync()
			}
			if err != nil {
				fatalErr = fmt.Errorf("commit log record: %w", err)
			}
			request.result <- commandResult{position: position, err: err}
		case readCommand:
			records, nextCursor, hasMore, err := readBackwardFile(request.ctx, file, request.cursor, request.limit)
			request.result <- commandResult{records: records, nextCursor: nextCursor, hasMore: hasMore, err: err}
		case closeCommand:
			err := errors.Join(file.Sync(), file.Close())
			request.result <- commandResult{err: err}
			return
		default:
			request.result <- commandResult{err: errors.New("unknown JSONL writer command")}
		}
	}
}

func appendRecord(file *os.File, data []byte) (Position, error) {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return Position{}, errors.New("JSONL record must end with newline")
	}
	start, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return Position{}, err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		return Position{Offset: start, Length: int64(n)}, nil
	}
	rollbackErr := errors.Join(file.Truncate(start), seek(file, start))
	if rollbackErr != nil {
		return Position{}, fmt.Errorf("append failed and rollback failed: %w", errors.Join(writeErr, rollbackErr))
	}
	return Position{}, writeErr
}

func seek(file *os.File, offset int64) error {
	_, err := file.Seek(offset, io.SeekStart)
	return err
}

// Append 同步等待 Writer 完成完整 Record 的写入。
func (w *Writer) Append(ctx context.Context, data []byte) (Position, error) {
	return w.submit(ctx, command{kind: appendCommand, data: bytes.Clone(data)})
}

// CommitTerminal 连续执行 Terminal Record 的 Append 和 Sync。
func (w *Writer) CommitTerminal(ctx context.Context, data []byte) (Position, error) {
	return w.submit(ctx, command{kind: commitCommand, data: bytes.Clone(data)})
}

// ReadBackward 通过同一 Writer 读取，保证读取与追加在该 Agent 内按命令顺序执行。
func (w *Writer) ReadBackward(ctx context.Context, cursor *int64, limit int) ([]Record, *int64, bool, error) {
	w.closeMu.Lock()
	defer w.closeMu.Unlock()
	if w.closed {
		return nil, nil, false, w.closeErr
	}
	request := command{
		ctx:    ctx,
		kind:   readCommand,
		cursor: cursor,
		limit:  limit,
		result: make(chan commandResult, 1),
	}
	select {
	case w.requests <- request:
	case <-ctx.Done():
		return nil, nil, false, ctx.Err()
	}
	result := <-request.result
	return result.records, result.nextCursor, result.hasMore, result.err
}

// Sync 等待同一 Writer 完成文件同步。
func (w *Writer) Sync(ctx context.Context) error {
	_, err := w.submit(ctx, command{kind: syncCommand})
	return err
}

func (w *Writer) submit(ctx context.Context, request command) (Position, error) {
	w.closeMu.Lock()
	defer w.closeMu.Unlock()
	if w.closed {
		return Position{}, w.closeErr
	}
	request.result = make(chan commandResult, 1)
	select {
	case w.requests <- request:
	case <-ctx.Done():
		return Position{}, ctx.Err()
	}
	result := <-request.result
	return result.position, result.err
}

// Close 同步关闭 Writer 持有的文件；调用方可以重复调用。
func (w *Writer) Close(ctx context.Context) error {
	w.closeMu.Lock()
	defer w.closeMu.Unlock()
	if w.closed {
		return w.closeErr
	}
	request := command{kind: closeCommand, result: make(chan commandResult, 1)}
	select {
	case w.requests <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	result := <-request.result
	w.closed = true
	w.closeErr = result.err
	return result.err
}

func readBackwardFile(ctx context.Context, file *os.File, cursor *int64, limit int) ([]Record, *int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	if limit <= 0 {
		return nil, nil, false, errors.New("history page limit must be positive")
	}
	info, err := file.Stat()
	if err != nil {
		return nil, nil, false, err
	}
	end := info.Size()
	if cursor != nil {
		end = *cursor
		if err := validateCursor(file, info.Size(), end); err != nil {
			return nil, nil, false, err
		}
	}
	if end == 0 {
		return nil, nil, false, nil
	}
	var last [1]byte
	if _, err := file.ReadAt(last[:], end-1); err != nil {
		return nil, nil, false, err
	}
	if last[0] != '\n' {
		return nil, nil, false, fmt.Errorf("log offset %d is not a complete record boundary", end)
	}

	records := make([]Record, 0, limit)
	for len(records) < limit && end > 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		start, err := previousRecordStart(file, end)
		if err != nil {
			return nil, nil, false, err
		}
		data := make([]byte, end-start)
		if _, err := file.ReadAt(data, start); err != nil {
			return nil, nil, false, err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			return nil, nil, false, fmt.Errorf("log offset %d contains an incomplete record", start)
		}
		records = append(records, Record{
			Position: Position{Offset: start, Length: int64(len(data))},
			Data:     data,
		})
		end = start
	}
	slices.Reverse(records)
	hasMore := end > 0
	var nextCursor *int64
	if hasMore {
		nextCursor = new(end)
	}
	return records, nextCursor, hasMore, nil
}

func validateCursor(file *os.File, fileSize, cursor int64) error {
	if cursor < 0 || cursor > fileSize {
		return fmt.Errorf("invalid history cursor %d", cursor)
	}
	if cursor == 0 {
		return nil
	}
	var previous [1]byte
	if _, err := file.ReadAt(previous[:], cursor-1); err != nil {
		return fmt.Errorf("validate history cursor %d: %w", cursor, err)
	}
	if previous[0] != '\n' {
		return fmt.Errorf("history cursor %d is not a record boundary", cursor)
	}
	return nil
}

func previousRecordStart(file *os.File, end int64) (int64, error) {
	searchEnd := end - 1
	for searchEnd > 0 {
		start := max(int64(0), searchEnd-reverseScanBlockSize)
		block := make([]byte, searchEnd-start)
		n, err := file.ReadAt(block, start)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if n > 0 {
			if index := bytes.LastIndexByte(block[:n], '\n'); index >= 0 {
				return start + int64(index) + 1, nil
			}
		}
		searchEnd = start
	}
	return 0, nil
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
	if recoverTail {
		flags = os.O_RDWR
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := validateCursor(file, info.Size(), offset); err != nil {
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
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := validateCursor(file, info.Size(), position.Offset); err != nil {
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
