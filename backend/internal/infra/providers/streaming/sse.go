package streaming

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ErrUnexpectedEOF 表示远程流在明确的终止事件前结束。
var ErrUnexpectedEOF = errors.New("provider stream ended before a terminal event")

type SSEEvent struct {
	Type string
	Data string
	ID   string
}

type SSEReader struct {
	scanner *bufio.Scanner
}

func NewSSEReader(reader io.Reader) *SSEReader {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 2*1024*1024)
	return &SSEReader{scanner: scanner}
}

func (r *SSEReader) Next() (SSEEvent, error) {
	var event SSEEvent
	var data strings.Builder
	hasData := false
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if line == "" {
			if !hasData {
				continue
			}
			event.Data = data.String()
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		}
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
		return SSEEvent{}, err
	}
	if hasData {
		event.Data = data.String()
		return event, nil
	}
	return SSEEvent{}, io.EOF
}
