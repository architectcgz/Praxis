package compress

import (
	"context"
	appcontext "praxis/internal/core/context"
	"strings"
	"testing"
)

func TestCustomSummaryFormatAndProtectedContent(t *testing.T) {
	format := SummaryFormat{
		SystemPrompt:          "custom summary instruction",
		PreviousSummaryHeader: "PREVIOUS\n",
		TranscriptHeader:      "\nTRANSCRIPT\n",
		ResultPrefix:          "HISTORY\n",
	}
	original := appcontext.ModelContext{SystemPrompt: "system", Entries: []appcontext.ContextEntry{
		{Kind: appcontext.ContextEntryReference, Role: appcontext.ContextRoleUser, Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: strings.Repeat("history ", 900)}}},
		{
			Protected: true,
			Kind:      appcontext.ContextEntryUserInput,
			Role:      appcontext.ContextRoleUser,
			Content:   []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "preserve input"}},
		},
	}}
	calls := 0
	compressed, changed, err := (FullStrategy{}).Compress(context.Background(), original, Options{ContextWindow: 2048, MaxOutputTokens: 256, SummaryFormat: format}, func(_ context.Context, history appcontext.ModelContext, _ int) (string, error) {
		calls++
		if history.SystemPrompt != format.SystemPrompt || !strings.HasPrefix(history.Entries[0].Content[0].Text, format.PreviousSummaryHeader) || !strings.Contains(history.Entries[0].Content[0].Text, format.TranscriptHeader) {
			t.Fatal(history)
		}
		return "  compact summary  ", nil
	})
	if err != nil || !changed || calls == 0 {
		t.Fatal(changed, calls, err)
	}
	if len(compressed.Entries) != 2 || compressed.Entries[0].Content[0].Text != "HISTORY\n  compact summary  " || compressed.Entries[1].Content[0].Text != "preserve input" {
		t.Fatal(compressed)
	}
	if original.Entries[0].Kind != appcontext.ContextEntryReference {
		t.Fatal("original context mutated")
	}
}
