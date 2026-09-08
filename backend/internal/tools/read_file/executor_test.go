package readfile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainexecution "praxis/internal/domain/execution"
	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

func TestDefinitionAndStrictArguments(t *testing.T) {
	definition := Definition()
	if definition.Name != domainsecurity.ToolReadFile || !json.Valid(definition.InputSchema) {
		t.Fatalf("invalid read_file definition: %#v", definition)
	}
	root := t.TempDir()
	invocation := runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
		WorkspacePathSnapshot: root, ReadScopes: []string{root},
	}}
	for _, encodedInput := range []string{
		`{"path":".","limit":3}`,
		`{"path":".","offset":null}`,
		`{"path":".","unknown":true}`,
		`{"path":".","path":"src"}`,
		`{"path":"."} {"path":"."}`,
	} {
		_, err := Normalize(runtimecontract.ToolCall{
			Name: domainsecurity.ToolReadFile, Input: json.RawMessage(encodedInput),
		}, invocation)
		if err == nil {
			t.Fatalf("Normalize(%s) succeeded, want strict validation error", encodedInput)
		}
	}
	call, err := Normalize(runtimecontract.ToolCall{
		Name: domainsecurity.ToolReadFile, Input: json.RawMessage(`{"path":"notes.txt"}`),
	}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	var normalized arguments
	if err := json.Unmarshal(call.NormalizedArguments, &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Path != filepath.Join(root, "notes.txt") || normalized.Offset != 0 || normalized.Limit != defaultLimit {
		t.Fatalf("unexpected normalized arguments: %#v", normalized)
	}
}

func TestReadsBoundedContentWithContinuation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := authorizedCall(t, root, `{"path":"notes.txt","offset":6,"limit":4}`)
	result, err := Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	var toolOutput output
	if err := json.Unmarshal([]byte(result.Content), &toolOutput); err != nil {
		t.Fatal(err)
	}
	if toolOutput.Path != path || toolOutput.Offset != 6 || toolOutput.Limit != 4 ||
		toolOutput.Size != 11 || toolOutput.Content != "worl" {
		t.Fatalf("unexpected read_file output: %#v", toolOutput)
	}
	if toolOutput.NextOffset == nil || *toolOutput.NextOffset != 10 || !result.Truncated {
		t.Fatalf("missing continuation metadata: %#v", toolOutput)
	}
}

func TestPreservesUTF8CharacterBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "utf8.txt"), []byte("你好"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(context.Background(), authorizedCall(t, root, `{"path":"utf8.txt","limit":4}`))
	if err != nil {
		t.Fatal(err)
	}
	var toolOutput output
	if err := json.Unmarshal([]byte(result.Content), &toolOutput); err != nil {
		t.Fatal(err)
	}
	if toolOutput.Content != "你" || toolOutput.NextOffset == nil || *toolOutput.NextOffset != len([]byte("你")) {
		t.Fatalf("UTF-8 content was split or continuation is invalid: %#v", toolOutput)
	}
}

func TestRejectsScopeAndSymlinkEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideCall := authorizedCall(t, root, `{"path":"../outside"}`)
	if _, err := Execute(context.Background(), outsideCall); err == nil {
		t.Fatal("scope escape succeeded")
	}
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	linkCall := authorizedCall(t, root, `{"path":"outside-link"}`)
	if _, err := Execute(context.Background(), linkCall); err == nil {
		t.Fatal("symlink escape succeeded")
	}
}

func TestBoundsEscapedOutput(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat(`"`, maxLimit)
	if err := os.WriteFile(filepath.Join(root, "quoted.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(context.Background(), authorizedCall(t, root, `{"path":"quoted.txt","limit":51200}`))
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(result.Content)) > domainexecution.MaxInlineToolResultBytes {
		t.Fatalf("result size=%d exceeds limit", len([]byte(result.Content)))
	}
	var toolOutput output
	if err := json.Unmarshal([]byte(result.Content), &toolOutput); err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || toolOutput.NextOffset == nil || len(toolOutput.Content) >= len(content) {
		t.Fatalf("large escaped output was not bounded: %#v", toolOutput)
	}
}

func authorizedCall(t *testing.T, root, encodedInput string) runtimecontract.AuthorizedToolCall {
	t.Helper()
	call, err := Normalize(
		runtimecontract.ToolCall{Name: domainsecurity.ToolReadFile, Input: json.RawMessage(encodedInput)},
		runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
			WorkspacePathSnapshot: root, ReadScopes: []string{root},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return call
}
