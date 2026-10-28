package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type storedEvent struct {
	Sequence   uint64          `json:"seq"`
	ID         string          `json:"event_id"`
	Type       string          `json:"type"`
	Collection string          `json:"collection"`
	Key        string          `json:"key"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func newEvent(sequence uint64, typ, collection, key string, value any) (storedEvent, error) {
	if sequence == 0 {
		return storedEvent{}, errors.New("storage sequence exhausted")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return storedEvent{}, err
	}
	return storedEvent{
		Sequence:   sequence,
		ID:         fmt.Sprintf("event:%d", sequence),
		Type:       typ,
		Collection: collection,
		Key:        key,
		CreatedAt:  time.Now().UTC(),
		Payload:    data,
	}, nil
}

func encodeEvent(event storedEvent) ([]byte, error) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func decodeJSON(data []byte, target any) error {
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
