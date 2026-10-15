package compress

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"reflect"
	"strings"
	"testing"
)

func text(role appcontext.ContextRole, body string, protected bool) appcontext.ContextEntry {
	return appcontext.ContextEntry{
		Kind:      appcontext.ContextEntryProviderOutput,
		Role:      role,
		Protected: protected,
		Content:   []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: body}},
	}
}
func chain(id string) []appcontext.ContextEntry {
	return []appcontext.ContextEntry{
		{Kind: appcontext.ContextEntryProviderOutput, Role: appcontext.ContextRoleAssistant, Content: []appcontext.ContextBlock{{
			Kind:   appcontext.ContextBlockToolCall,
			CallID: id,
			Name:   "tool",
			Input:  []byte(`{}`),
		}}},
		{Kind: appcontext.ContextEntryToolResult, Role: appcontext.ContextRoleTool, Content: []appcontext.ContextBlock{appcontext.NewToolResultBlock(id, "tool", "result", false)}},
	}
}

func TestStrategiesPreserveProtectedOrderAndIncompleteChains(t *testing.T) {
	for _, strategy := range []Strategy{FullStrategy{}, PartialStrategy{}} {
		t.Run(reflect.TypeOf(strategy).Name(), func(t *testing.T) {
			value := appcontext.ModelContext{SystemPrompt: "system", Entries: []appcontext.ContextEntry{
				text(appcontext.ContextRoleUser, "shared", true),
				text(appcontext.ContextRoleUser, strings.Repeat("old history ", 2500), false),
				text(appcontext.ContextRoleUser, "middle protected", true),
				text(appcontext.ContextRoleAssistant, strings.Repeat("older answer ", 2500), false),
			}}
			value.Entries = append(value.Entries, chain("closed")...)
			value.Entries = append(value.Entries, text(appcontext.ContextRoleAssistant, "closed final", false))
			value.Entries = append(value.Entries, text(appcontext.ContextRoleUser, "latest", true))
			value.Entries = append(value.Entries, chain("open")...)
			// 即使结果到齐，没有最终 assistant 回答仍属未闭合工具链。
			original := value.Clone()
			compressed, changed, err := strategy.Compress(t.Context(), value, Options{ContextWindow: 8192, MaxOutputTokens: 1024}, func(context.Context, appcontext.ModelContext, int) (string, error) { return "摘要", nil })
			if err != nil || !changed {
				t.Fatalf("压缩失败: %v %v", changed, err)
			}
			if !reflect.DeepEqual(value, original) {
				t.Fatal("压缩修改原始上下文")
			}
			var protected []string
			for _, entry := range compressed.Entries {
				if entry.Protected {
					protected = append(protected, entry.Content[0].Text)
				}
			}
			if !reflect.DeepEqual(protected, []string{"shared", "middle protected", "latest"}) {
				t.Fatalf("保护原文丢失或乱序: %v", protected)
			}
			if !reflect.DeepEqual(compressed.Entries[len(compressed.Entries)-2:], value.Entries[len(value.Entries)-2:]) {
				t.Fatal("未闭合工具链被压缩")
			}
		})
	}
}

func TestCompressionNeverSendsReasoningStateToSummary(t *testing.T) {
	state := &contracts.ReasoningState{Protocol: contracts.ProtocolOpenAIResponses, Version: contracts.ReasoningStateVersion, Payload: []byte(`{"encrypted_content":"secret"}`)}
	value := appcontext.ModelContext{Entries: []appcontext.ContextEntry{
		text(appcontext.ContextRoleUser, strings.Repeat("history ", 6000), false),
		{Kind: appcontext.ContextEntryProviderOutput, Role: appcontext.ContextRoleAssistant, Content: []appcontext.ContextBlock{
			{Kind: appcontext.ContextBlockReasoningState, ReasoningState: state},
			{Kind: appcontext.ContextBlockText, Text: "finished"},
		}},
		text(appcontext.ContextRoleUser, "current", true),
	}}
	value.Entries = append(value.Entries, chain("pending")...)
	value.Entries[len(value.Entries)-2].Content = append(value.Entries[len(value.Entries)-2].Content, appcontext.ContextBlock{Kind: appcontext.ContextBlockReasoningState, ReasoningState: state.Clone()})
	compressed, _, err := (FullStrategy{}).Compress(t.Context(), value, Options{ContextWindow: 8192, MaxOutputTokens: 1024}, func(_ context.Context, history appcontext.ModelContext, _ int) (string, error) {
		data, _ := json.Marshal(history)
		if strings.Contains(string(data), "secret") || strings.Contains(string(data), "reasoning_state") {
			t.Fatal("推理状态泄漏给摘要模型")
		}
		return "摘要", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range compressed.Entries[:len(compressed.Entries)-2] {
		for _, block := range entry.Content {
			if block.ReasoningState != nil {
				t.Fatal("已摘要前缀残留推理状态")
			}
		}
	}
	if !reflect.DeepEqual(compressed.Entries[len(compressed.Entries)-2:], value.Entries[len(value.Entries)-2:]) {
		t.Fatal("保留区推理状态被修改")
	}
}

func TestCompressionCannotDropProtectedOrSplitProtectedToolChain(t *testing.T) {
	entries := chain("protected")
	entries = append(entries, text(appcontext.ContextRoleAssistant, "final", false))
	entries[1].Protected = true
	flags := protectedEntries(appcontext.ModelContext{Entries: entries}, false)
	if !reflect.DeepEqual(flags, []bool{true, true, true}) {
		t.Fatalf("保护标记未扩展到整条工具链: %v", flags)
	}
	value := appcontext.ModelContext{Entries: []appcontext.ContextEntry{text(appcontext.ContextRoleUser, strings.Repeat("protected ", 10000), true)}}
	_, _, err := (FullStrategy{}).Compress(t.Context(), value, Options{ContextWindow: 2048, MaxOutputTokens: 256}, nil)
	var window *appcontext.ContextWindowExceededError
	if !errors.As(err, &window) {
		t.Fatalf("不可压缩内容超窗未失败: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := (FullStrategy{}).Compress(ctx, value, Options{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("忽略取消: %v", err)
	}
}
