package shared

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecodeOptionalInteger(t *testing.T) {
	tests := []struct {
		name    string
		encoded json.RawMessage
		initial int
		want    int
		wantErr bool
	}{
		{name: "omitted", initial: 7, want: 7},
		{name: "integer", encoded: json.RawMessage(`42`), want: 42},
		{name: "null", encoded: json.RawMessage(`null`), wantErr: true},
		{name: "string", encoded: json.RawMessage(`"42"`), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := test.initial
			err := DecodeOptionalInteger(test.encoded, &value)
			if (err != nil) != test.wantErr {
				t.Fatalf("DecodeOptionalInteger(%s) error = %v, wantErr %t", test.encoded, err, test.wantErr)
			}
			if value != test.want {
				t.Fatalf("DecodeOptionalInteger(%s) value = %d, want %d", test.encoded, value, test.want)
			}
		})
	}
}

func TestRejectTrailingJSON(t *testing.T) {
	for _, test := range []struct {
		name    string
		encoded string
		wantErr bool
	}{
		{name: "end of input", encoded: ``},
		{name: "second value", encoded: `{"other":true}`, wantErr: true},
		{name: "malformed value", encoded: `{`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := json.NewDecoder(bytes.NewBufferString(test.encoded))
			err := RejectTrailingJSON(decoder)
			if (err != nil) != test.wantErr {
				t.Fatalf("RejectTrailingJSON(%q) error = %v, wantErr %t", test.encoded, err, test.wantErr)
			}
		})
	}
}
