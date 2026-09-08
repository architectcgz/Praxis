package listdir

import (
	"context"
	"encoding/json"
	"fmt"
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
	if definition.Name != domainsecurity.ToolListDir || !json.Valid(definition.InputSchema) {
		t.Fatalf("invalid list_dir definition: %#v", definition)
	}
	root := t.TempDir()
	invocation := runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
		WorkspacePathSnapshot: root, ReadScopes: []string{root},
	}}
	for _, encodedInput := range []string{
		`{"path":".","limit":0}`,
		`{"path":".","offset":null}`,
		`{"path":".","unknown":true}`,
		`{"path":".","path":"src"}`,
		`{"path":"."} {"path":"."}`,
	} {
		_, err := Normalize(runtimecontract.ToolCall{
			Name: domainsecurity.ToolListDir, Input: json.RawMessage(encodedInput),
		}, invocation)
		if err == nil {
			t.Fatalf("Normalize(%s) succeeded, want strict validation error", encodedInput)
		}
	}
	call, err := Normalize(runtimecontract.ToolCall{
		Name: domainsecurity.ToolListDir, Input: json.RawMessage(`{"path":"."}`),
	}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	var normalized arguments
	if err := json.Unmarshal(call.NormalizedArguments, &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Path != filepath.Clean(root) || normalized.Offset != 0 || normalized.Limit != defaultLimit {
		t.Fatalf("unexpected normalized arguments: %#v", normalized)
	}
}

func TestReturnsSortedPaginatedEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "zeta.txt"), []byte("z"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "middle.txt"), []byte("m"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := authorizedCall(t, root, `{"path":".","offset":1,"limit":1}`)
	result, err := Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	var toolOutput output
	if err := json.Unmarshal([]byte(result.Content), &toolOutput); err != nil {
		t.Fatal(err)
	}
	if toolOutput.Total != 3 || len(toolOutput.Entries) != 1 ||
		toolOutput.Entries[0].Name != "middle.txt" || toolOutput.Entries[0].Type != "file" {
		t.Fatalf("unexpected list_dir output: %#v", toolOutput)
	}
	if toolOutput.NextOffset == nil || *toolOutput.NextOffset != 2 || !result.Truncated {
		t.Fatalf("missing pagination metadata: %#v", toolOutput)
	}
}

func TestRejectsScopeAndSymlinkEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
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

func TestBoundsLargeOutput(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 400; index++ {
		name := fmt.Sprintf("%03d_%s", index, strings.Repeat("x", 150))
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	call := authorizedCall(t, root, `{"path":".","limit":1000}`)
	result, err := Execute(context.Background(), call)
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
	if !result.Truncated || toolOutput.NextOffset == nil || len(toolOutput.Entries) >= toolOutput.Total {
		t.Fatalf("large output was not paginated: entries=%d total=%d", len(toolOutput.Entries), toolOutput.Total)
	}
}

func authorizedCall(t *testing.T, root, encodedInput string) runtimecontract.AuthorizedToolCall {
	t.Helper()
	call, err := Normalize(
		runtimecontract.ToolCall{Name: domainsecurity.ToolListDir, Input: json.RawMessage(encodedInput)},
		runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
			WorkspacePathSnapshot: root, ReadScopes: []string{root},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return call
}
