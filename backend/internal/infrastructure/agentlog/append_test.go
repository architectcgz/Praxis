package agentlog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
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
	if err := store.AppendStructuredMessage(t.Context(), "other-execution", "future", "assistant", "request", blocks); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendStructuredMessage(t.Context(), "current-execution", "current", "user", "current-request", blocks); err != nil {
		t.Fatal(err)
	}
	selected, err := store.ListExecutionMessages(t.Context(), []domainexecution.TranscriptMessageRef{{
		Sequence: messages[0].Sequence, ExecutionID: messages[0].ExecutionID,
		MessageID: messages[0].MessageID, Digest: messages[0].Digest,
	}}, "current-execution")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].MessageID != "assistant:1" || selected[1].MessageID != "current" {
		t.Fatalf("unexpected execution transcript: %#v", selected)
	}
	badReference := domainexecution.TranscriptMessageRef{
		Sequence: messages[0].Sequence, ExecutionID: messages[0].ExecutionID,
		MessageID: messages[0].MessageID, Digest: "changed",
	}
	if _, err := store.ListExecutionMessages(t.Context(), []domainexecution.TranscriptMessageRef{badReference}, "current-execution"); err == nil {
		t.Fatal("changed transcript digest should fail")
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

func TestListContextArtifactsUsesSelectedEntryIDs(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root+"/session.jsonl", root+"/temporary")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(context.Background()) }()
	if err := store.Initialize(t.Context(), sessionport.AgentSessionHeader{
		SessionID: "session", AgentID: "agent", WorkspaceID: "workspace",
		Profile: domainsecurity.ProfilePrimary, InjectionNonce: "nonce", MinReaderVersion: 1, WrittenBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	first, err := store.AppendContextArtifact(t.Context(), sessionport.ContextArtifact{
		DeliveryID: "delivery-1", Kind: "briefing", ArtifactID: "briefing-1", Body: json.RawMessage(`{"value":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendContextArtifact(t.Context(), sessionport.ContextArtifact{
		DeliveryID: "delivery-2", Kind: "note", ArtifactID: "note-1", Body: json.RawMessage(`{"value":2}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := store.ListContextArtifacts(t.Context(), []string{second.EntryID, first.EntryID})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || artifacts[0].DeliveryID != "delivery-2" || artifacts[1].DeliveryID != "delivery-1" {
		t.Fatalf("unexpected selected artifacts: %#v", artifacts)
	}
	if _, err := store.ListContextArtifacts(t.Context(), []string{"missing-entry"}); err == nil {
		t.Fatal("missing artifact entry should fail")
	}
}
