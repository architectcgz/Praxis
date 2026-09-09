package agentlog

import (
	"context"
	"errors"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	"testing"

	sessionport "praxis/internal/session"
)

func TestAppendStructuredMessageUsesLogicalMessageID(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root+"/session.jsonl", root+"/temporary")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(context.Background()) }()
	if err := store.Initialize(context.Background(), sessionport.AgentSessionHeader{
		SessionID: "session", AgentID: "agent", WorkspaceID: "workspace",
		Profile: domainsecurity.ProfilePrimary, InjectionNonce: "nonce", MinReaderVersion: 1, WrittenBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	blocks := []sessionport.TranscriptContentBlock{{Kind: "text", Text: "hello"}}
	if err := store.AppendStructuredMessage(context.Background(), "execution", "assistant:1", "assistant", "request", blocks); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendStructuredMessage(context.Background(), "execution", "assistant:1", "assistant", "request", blocks); err != nil {
		t.Fatalf("same logical message should be idempotent: %v", err)
	}
	if err := store.AppendStructuredMessage(context.Background(), "execution", "assistant:2", "assistant", "request", blocks); err != nil {
		t.Fatalf("different logical messages may have equal content: %v", err)
	}
	if err := store.AppendStructuredMessage(context.Background(), "execution", "assistant:1", "assistant", "request", []sessionport.TranscriptContentBlock{{Kind: "text", Text: "changed"}}); !errors.Is(err, domainfoundation.ErrRequestConflict) {
		t.Fatalf("same logical message with changed content should conflict, got %v", err)
	}
	messages, err := store.ListMessages(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].MessageID != "assistant:1" || messages[1].MessageID != "assistant:2" {
		t.Fatalf("unexpected transcript messages: %#v", messages)
	}
}

func TestAppendStructuredMessageAcceptsToolRole(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root+"/session.jsonl", root+"/temporary")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(context.Background()) }()
	if err := store.Initialize(context.Background(), sessionport.AgentSessionHeader{
		SessionID: "session", AgentID: "agent", WorkspaceID: "workspace",
		Profile: domainsecurity.ProfilePrimary, InjectionNonce: "nonce", MinReaderVersion: 1, WrittenBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	blocks := []sessionport.TranscriptContentBlock{{
		Kind: "tool_result", ToolCallID: "call-1", ToolName: "list_dir", Text: `{"entries":[]}`,
	}}
	if err := store.AppendStructuredMessage(
		context.Background(), "execution", "tool-result:1", "tool", "request", blocks,
	); err != nil {
		t.Fatal(err)
	}
	messages, err := store.ListMessages(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "tool" {
		t.Fatalf("unexpected tool transcript message: %#v", messages)
	}
}
