package jsonl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReplayPositionsAndCompleteIdentity(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	f.restart(t)
	stored, err := f.repos.Tasks.FindByRequest(context.Background(), "agent", "request")
	requireOK(t, err)
	if stored.ID != task.ID || stored.SessionID != "session" {
		t.Fatal(stored)
	}
	if len(f.store.messages.sessions) != 0 {
		t.Fatal("startup must not preload all message indexes")
	}
	ref := f.store.index.latest[entityKey{"task", task.ID.String()}].location
	record, err := f.store.readRecord(ref, nil)
	requireOK(t, err)
	var payload map[string]json.RawMessage
	requireOK(t, json.Unmarshal(record.Events[ref.EventIndex].Payload, &payload))
	if len(payload["ID"]) == 0 || len(payload["SessionID"]) == 0 {
		t.Fatal("log must preserve complete identity")
	}
}

func TestReplayIncompleteTailAndCorruptCompleteLine(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	path, err := f.store.path("session")
	requireOK(t, err)
	requireOK(t, f.store.Close(context.Background()))
	data, err := os.ReadFile(path)
	requireOK(t, err)
	tail := []byte(`{"events":[{"seq":`)
	requireOK(t, os.WriteFile(path, append(data, tail...), 0600))
	f.restart(t)
	backups, err := filepath.Glob(filepath.Join(f.root, "incomplete-tail-*"))
	requireOK(t, err)
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	backup, err := os.ReadFile(backups[0])
	requireOK(t, err)
	if string(backup) != string(tail) {
		t.Fatal("incomplete tail must be preserved in full")
	}
	stored, err := f.repos.Tasks.Get(context.Background(), task.ID)
	requireOK(t, err)
	if stored.ID != task.ID {
		t.Fatal(stored)
	}
	requireOK(t, f.store.Close(context.Background()))
	corrupt := append(data, []byte("{invalid}\n")...)
	requireOK(t, os.WriteFile(path, corrupt, 0600))
	if store, err := Open(context.Background(), f.root); err == nil {
		_ = store.Close(context.Background())
		t.Fatal("a complete corrupt line must prevent startup")
	}
	after, err := os.ReadFile(path)
	requireOK(t, err)
	if string(after) != string(corrupt) {
		t.Fatal("a complete corrupt line must not be silently removed")
	}
}

func TestFormatRejection(t *testing.T) {
	for _, tc := range []struct{ name, marker, data string }{
		{name: "missing", data: "{}\n"},
		{name: "old_version", marker: `{"format":"praxis-jsonl","version":1}`},
		{name: "unknown_field", marker: `{"format":"praxis-jsonl","version":2,"compat":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.marker != "" {
				requireOK(t, os.WriteFile(filepath.Join(root, "storage.json"), []byte(tc.marker), 0600))
			}
			if tc.data != "" {
				requireOK(t, os.WriteFile(filepath.Join(root, "projects.jsonl"), []byte(tc.data), 0600))
			}
			if s, err := Open(context.Background(), root); err == nil {
				_ = s.Close(context.Background())
				t.Fatal("format must be explicitly rejected")
			}
		})
	}
}

func TestReplayRejectsSequenceAndPayloadMismatch(t *testing.T) {
	for _, corrupt := range []string{"sequence", "identity", "unknown"} {
		t.Run(corrupt, func(t *testing.T) {
			f := newStoreFixture(t)
			requireOK(t, f.store.Close(context.Background()))
			path := filepath.Join(f.root, "projects.jsonl")
			data, err := os.ReadFile(path)
			requireOK(t, err)
			var record batch
			requireOK(t, decodeJSON(data, &record))
			switch corrupt {
			case "sequence":
				record.Events[0].Sequence++
			case "identity":
				record.Events[0].Key = "different"
			case "unknown":
				var object map[string]any
				requireOK(t, json.Unmarshal(record.Events[0].Payload, &object))
				object["oldField"] = true
				record.Events[0].Payload, err = json.Marshal(object)
				requireOK(t, err)
			}
			data, err = json.Marshal(record)
			requireOK(t, err)
			requireOK(t, os.WriteFile(path, append(data, '\n'), 0600))
			if s, err := Open(context.Background(), f.root); err == nil {
				_ = s.Close(context.Background())
				t.Fatal("corrupt identity or protocol must be rejected")
			}
		})
	}
}
