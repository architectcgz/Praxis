package start

import (
	"context"
	"errors"
	"testing"

	commandprotocol "praxis/internal/command"
	sessionport "praxis/internal/session"
)

type readyReadiness bool

func (r readyReadiness) Ready() bool {
	return bool(r)
}

func TestSendInputRequiresExplicitModelSelection(t *testing.T) {
	service := &Service{readiness: readyReadiness(true)}
	for _, params := range []SendInputParams{
		{SessionID: "session", RequestID: "request", Content: "hello", ModelID: "model"},
		{SessionID: "session", RequestID: "request", Content: "hello", ProviderID: "provider"},
	} {
		_, err := service.SendInput(context.Background(), params)
		var commandError *commandprotocol.Error
		if !errors.As(err, &commandError) || commandError.Code != commandprotocol.ErrorModelNotConfigured {
			t.Fatalf("SendInput() error = %v, want %s", err, commandprotocol.ErrorModelNotConfigured)
		}
	}
}

func TestSelectTranscriptMessagesKeepsExecutionGroupsWhole(t *testing.T) {
	messages := []sessionport.AgentSessionMessage{
		{ExecutionID: "execution-1", MessageID: "one", Blocks: []sessionport.TranscriptContentBlock{{Text: "a"}}},
		{ExecutionID: "execution-1", MessageID: "two", Blocks: []sessionport.TranscriptContentBlock{{Text: "b"}}},
		{ExecutionID: "execution-2", MessageID: "three", Blocks: []sessionport.TranscriptContentBlock{{Text: "large"}}},
		{ExecutionID: "execution-2", MessageID: "four", Blocks: []sessionport.TranscriptContentBlock{{Text: "large"}}},
	}
	selected := selectTranscriptMessages(messages, 2)
	if len(selected) != 2 || selected[0].MessageID != "one" || selected[1].MessageID != "two" {
		t.Fatalf("unexpected transcript selection: %#v", selected)
	}
}

func TestSelectArtifactEntryRefsKeepsRequiredAndRecentArtifacts(t *testing.T) {
	artifacts := []sessionport.AgentContextArtifact{
		{EntryID: "old", Body: []byte("old")},
		{EntryID: "recent", Body: []byte("recent")},
		{EntryID: "required", Body: []byte("required-over-budget")},
	}
	selected, err := selectArtifactEntryRefs(artifacts, []string{"required"}, len("recent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0] != "required" {
		t.Fatalf("unexpected artifact selection: %#v", selected)
	}
	if _, err := selectArtifactEntryRefs(artifacts, []string{"missing"}, 1024); err == nil {
		t.Fatal("missing required artifact should fail")
	}
}
