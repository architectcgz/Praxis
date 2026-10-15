package jsonl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnsupportedTaskPayloadFailsWithoutRewritingLog(t *testing.T) {
	for _, payload := range []string{
		`{"ID":"task","Input":{},"Status":"pending"}`,
		`{"ID":"task","InputMessageID":" input ","Status":"pending"}`,
	} {
		t.Run(payload, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "sessions"), 0700); err != nil {
				t.Fatal(err)
			}
			record, err := json.Marshal(batch{Events: []Event{{
				Sequence:   1,
				ID:         "event:1",
				Type:       "task.saved",
				Collection: "task",
				Key:        "task",
				CreatedAt:  time.Now().UTC(),
				Payload:    json.RawMessage(payload),
			}}})
			if err != nil {
				t.Fatal(err)
			}
			record = append(record, '\n')
			path := filepath.Join(root, "sessions", "session.jsonl")
			if err := os.WriteFile(path, record, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := Open(t.Context(), root)
			if err == nil {
				store.Close(t.Context())
				t.Fatal("接受不支持或不规范的 Task 数据")
			}
			if !strings.Contains(err.Error(), "seq=1") {
				t.Fatalf("错误没有定位日志记录: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(record, after) {
				t.Fatalf("恢复失败修改或删除原始数据: %v", err)
			}
		})
	}
}
