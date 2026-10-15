package context

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func textEntry(text string, protected bool) ContextEntry {
	return ContextEntry{
		Kind:      ContextEntryUserInput,
		Role:      ContextRoleUser,
		Protected: protected,
		Content:   []ContextBlock{{Kind: ContextBlockText, Text: text}},
	}
}

func TestBuilderOrderCopyNormalizationAndCapacity(t *testing.T) {
	content := Content{SystemPrompt: "system", Entries: []ContextEntry{
		textEntry(strings.Repeat("history ", 2000), false),
		{Kind: ContextEntryProviderOutput, Role: ContextRoleAssistant, Content: []ContextBlock{
			{Kind: ContextBlockThinking, Text: "display only"},
			{
				Kind:   ContextBlockToolCall,
				CallID: "call",
				Name:   "tool",
				Input:  []byte(`{"x":1}`),
			},
		}},
		textEntry("  preserve original text  ", true),
	}}
	built, err := (ContextBuilder{}).Build(content, Limits{ContextWindow: 2048, MaxOutputTokens: 256})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Entries) != 3 || built.Entries[2].Content[0].Text != "  preserve original text  " || len(built.Entries[1].Content) != 1 {
		t.Fatalf("顺序、原文或思考过滤错误: %+v", built)
	}
	built.Entries[1].Content[0].Input[0] = '['
	if content.Entries[1].Content[1].Input[0] != '{' {
		t.Fatal("构建结果共享原始参数内存")
	}
	// 历史超窗可交给压缩处理，必要原文超窗则必须拒绝。
	content.Entries[0].Protected = true
	_, err = (ContextBuilder{}).Build(content, Limits{ContextWindow: 2048, MaxOutputTokens: 256})
	var window *ContextWindowExceededError
	if !errors.As(err, &window) {
		t.Fatalf("必要原文超窗未被拒绝: %v", err)
	}
	if _, err := (ContextBuilder{}).Build(Content{}, Limits{ContextWindow: 100, MaxOutputTokens: 100}); err == nil {
		t.Fatal("接受非法输出预留")
	}
	if _, err := (ContextBuilder{}).Build(Content{}, Limits{ContextWindow: 2048}); err == nil {
		t.Fatal("接受空上下文")
	}
}

func TestRestoreToolResultPreservesDeclaredOrderAndIdentity(t *testing.T) {
	value := ModelContext{Entries: []ContextEntry{
		{Kind: ContextEntryProviderOutput, Role: ContextRoleAssistant, Content: []ContextBlock{
			{Kind: ContextBlockToolCall, CallID: "a", Name: "tool"},
			{Kind: ContextBlockToolCall, CallID: "b", Name: "tool"},
			{Kind: ContextBlockToolCall, CallID: "c", Name: "tool"},
		}},
	}}
	value = value.AppendToolResult(NewToolResultBlock("a", "tool", "first", false))
	value = value.AppendToolResult(NewToolResultBlock("c", "tool", "third", false))
	value = value.RestoreToolResult(NewToolResultBlock("b", "tool", "second", false))
	value = value.RestoreToolResult(NewToolResultBlock("b", "tool", "duplicate", false))
	var ids []string
	for _, entry := range value.Entries[1:] {
		ids = append(ids, entry.Content[0].CallID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b", "c"}) {
		t.Fatalf("恢复顺序或去重错误: %v", ids)
	}
}
