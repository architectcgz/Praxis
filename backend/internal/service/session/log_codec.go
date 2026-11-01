package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

type storedEvent struct {
	Type       string          `json:"type"`
	Collection string          `json:"collection"`
	Key        string          `json:"key"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload"`
}

func newEvent(typ, collection, key string, value any) (storedEvent, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return storedEvent{}, err
	}
	return storedEvent{
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
