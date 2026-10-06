package jsonl

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

func TestRestoredTaskNormalization(t *testing.T) {
	fields := []struct {
		name string
		set  func(*taskmodel.Task, string)
	}{
		{"task.id", func(v *taskmodel.Task, s string) { v.ID = contracts.TaskID(s) }},
		{"task.agentID", func(v *taskmodel.Task, s string) { v.AgentID = contracts.AgentID(s) }},
		{"task.requestID", func(v *taskmodel.Task, s string) { v.RequestID = contracts.RequestID(s) }},
		{"task.providerID", func(v *taskmodel.Task, s string) { v.ProviderID = s }},
		{"task.modelID", func(v *taskmodel.Task, s string) { v.ModelID = s }},
		{"task.reasoningLevel", func(v *taskmodel.Task, s string) { v.ReasoningLevel = s }},
		{"task.failureMessage", func(v *taskmodel.Task, s string) { v.FailureMessage = s }},
		{"task.input.agentDefinitionRevision", func(v *taskmodel.Task, s string) { v.Input.AgentDefinitionRevision = s }},
		{"task.input.contextDigest", func(v *taskmodel.Task, s string) { v.Input.ContextDigest = s }},
		{"task.input.currentInputMessageID", func(v *taskmodel.Task, s string) { v.Input.CurrentInputMessageID = s }},
	}
	for _, field := range fields {
		for _, value := range []string{"正常值", "", "内容中间 保留空格\n以及换行", " 前置空格", "后置空格\t", "\u3000\t\n"} {
			t.Run(field.name+"/"+value, func(t *testing.T) {
				// 只检查恢复输入的规范性；必填和关联关系仍由投影的业务校验负责。
				task := taskmodel.Task{ID: "task", AgentID: "agent", RequestID: "request"}
				field.set(&task, value)
				payload, err := encode("task", task)
				if err != nil {
					t.Fatal(err)
				}
				event := Event{
					Sequence:   1,
					ID:         "event:1",
					Type:       "task.saved",
					Collection: "task",
					Key:        "task",
					CreatedAt:  time.Now().UTC(),
					Payload:    payload,
				}
				if value == strings.TrimSpace(value) {
					before := bytes.Clone(payload)
					if err := validateRestoredEvent("session", event); err != nil {
						t.Fatalf("规范化字段不应被拒绝: %v", err)
					}
					if !bytes.Equal(before, payload) {
						t.Fatal("恢复校验不能修改原始数据")
					}
					return
				}

				root := t.TempDir()
				if err := os.Mkdir(filepath.Join(root, "sessions"), 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "sessions", "session.jsonl")
				data, err := json.Marshal(batch{Events: []Event{event}})
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, '\n')
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				store := &Store{
					root:      root,
					objects:   make(map[objectKey]object),
					sequences: make(map[string]uint64),
				}
				err = store.replay(path)
				var validation *contracts.ValidationError
				if !errors.As(err, &validation) || validation.Field != field.name {
					t.Fatalf("应在恢复入口拒绝 %s，实际错误: %v", field.name, err)
				}
				if len(store.objects) != 0 || store.sequences["session"] != 0 {
					t.Fatal("非规范化字段不能进入投影或推进恢复序号")
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, after) {
					t.Fatal("恢复失败不能修改完整日志记录")
				}
			})
		}
	}
}

func TestUnsupportedRecordsLeaveCompleteLogUnchanged(t *testing.T) {
	for _, format := range []string{"queue", "turn", "task-with-work-reference"} {
		t.Run(format, func(t *testing.T) {
			root := filepath.Clean(t.TempDir())
			if err := os.Mkdir(filepath.Join(root, "sessions"), 0o700); err != nil {
				t.Fatal(err)
			}
			collection := format
			payload := json.RawMessage(`{"ID":"object"}`)
			if format == "task-with-work-reference" {
				collection = "task"
				payload = json.RawMessage(`{"ID":"object","WorkItemID":"work"}`)
			}
			data, err := json.Marshal(batch{Events: []Event{{
				Sequence:   1,
				ID:         "event:1",
				Type:       collection + ".saved",
				Collection: collection,
				Key:        "object",
				CreatedAt:  time.Now().UTC(),
				Payload:    payload,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n')
			path := filepath.Join(root, "sessions", "session.jsonl")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := Open(t.Context(), root)
			if err == nil {
				_ = store.Close(t.Context())
				t.Fatal("不支持的记录必须拒绝恢复")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, after) {
				t.Fatal("恢复失败不能重写或删除完整日志")
			}
		})
	}
}
