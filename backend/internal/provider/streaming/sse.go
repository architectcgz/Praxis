package streaming

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
)

// ErrUnexpectedEOF 表示远程流在协议终态前中断。
var ErrUnexpectedEOF = errors.New("provider stream ended before a terminal event")
var ErrFrameTooLarge = errors.New("SSE frame exceeds size limit")

const DefaultMaxFrameBytes = 2 * 1024 * 1024

type SSEEvent struct {
	Type string
	Data string
	ID   string
}

type SSEReader struct {
	scanner       *bufio.Scanner
	maxFrameBytes int
	firstLine     bool
}

// NewSSEReader 创建使用默认单帧大小限制的 SSE reader。
func NewSSEReader(reader io.Reader) *SSEReader {
	result, _ := NewSSEReaderWithLimit(reader, DefaultMaxFrameBytes)
	return result
}

// NewSSEReaderWithLimit 限制整帧字节数（含注释和多行数据），非正数限制返回错误。
func NewSSEReaderWithLimit(reader io.Reader, maxFrameBytes int) (*SSEReader, error) {
	if reader == nil || maxFrameBytes <= 0 {
		return nil, errors.New("SSE reader and positive frame limit are required")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(1024, maxFrameBytes)), maxFrameBytes+2)
	scanner.Split(splitSSELine)
	return &SSEReader{scanner: scanner, maxFrameBytes: maxFrameBytes, firstLine: true}, nil
}

// Next 按 SSE 规则拼接 data；只有空行才提交事件，EOF 不提交未闭合帧。
// 只含 event/id 的空帧不会污染下一帧，注释与空数据事件不向协议层转发。
func (r *SSEReader) Next() (SSEEvent, error) {
	var event SSEEvent
	var data strings.Builder
	hasData, frameBytes := false, 0
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if r.firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			r.firstLine = false
		}
		frameBytes += len(line) + 1
		if frameBytes > r.maxFrameBytes {
			return SSEEvent{}, ErrFrameTooLarge
		}
		if line == "" {
			if hasData && data.Len() > 0 {
				event.Data = data.String()
				return event, nil
			}
			event = SSEEvent{}
			data.Reset()
			hasData, frameBytes = false, 0
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event.Type = value
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			hasData = true
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				event.ID = value
			}
		}
	}
	if err := r.scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return SSEEvent{}, ErrFrameTooLarge
		}
		return SSEEvent{}, err
	}
	return SSEEvent{}, io.EOF
}

func splitSSELine(data []byte, atEOF bool) (int, []byte, error) {
	if index := bytes.IndexAny(data, "\r\n"); index >= 0 {
		if data[index] == '\r' && index+1 == len(data) && !atEOF {
			return 0, nil, nil
		}
		advance := index + 1
		if data[index] == '\r' && advance < len(data) && data[advance] == '\n' {
			advance++
		}
		return advance, data[:index], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
