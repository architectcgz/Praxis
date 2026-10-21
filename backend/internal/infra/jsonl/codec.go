package jsonl

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

type Event struct {
	Sequence   uint64          `json:"seq"`
	ID         string          `json:"event_id"`
	Type       string          `json:"type"`
	Collection string          `json:"collection"`
	Key        string          `json:"key"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// Commit 是一个日志行中原子提交的事件集合。
type Commit struct {
	Events []Event `json:"events"`
}

func Decode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON data")
	}
	return nil
}
