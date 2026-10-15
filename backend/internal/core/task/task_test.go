package task

import (
	"praxis/internal/core/model"
	"reflect"
	"testing"
	"time"
)

func TestPreparationFreezesSelectionAndLeavesFailureUnchanged(t *testing.T) {
	task, err := NewPendingTask("task", "session", "agent", "request", "input", 1, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	original := task
	if err := task.Prepare(Config{}, ContextSelection{}); err == nil || !reflect.DeepEqual(original, task) {
		t.Fatal("准备失败改变了 Pending")
	}
	config := Config{AgentDefinitionID: "primary", WorkspacePath: t.TempDir(), Model: model.Config{
		ProviderID:      "provider",
		ModelID:         "model",
		APIFormat:       "openai_chat",
		BaseURL:         "https://example.com",
		ContextWindow:   8192,
		MaxOutputTokens: 1024,
		ReasoningLevels: []string{"high"},
	}}
	selection := ContextSelection{ExcludedMessageIDs: []string{"other"}, SessionContextRefs: []ContextReference{{ID: "shared", Digest: "digest"}}}
	if err := task.Prepare(config, selection); err != nil {
		t.Fatal(err)
	}
	config.Model.ReasoningLevels[0] = "changed"
	selection.ExcludedMessageIDs[0] = "changed"
	selection.SessionContextRefs[0].Digest = "changed"
	if task.Config.Model.ReasoningLevels[0] != "high" || task.ContextSelection.ExcludedMessageIDs[0] != "other" || task.ContextSelection.SessionContextRefs[0].Digest != "digest" {
		t.Fatal("冻结数据仍共享外部可变内存")
	}
}
