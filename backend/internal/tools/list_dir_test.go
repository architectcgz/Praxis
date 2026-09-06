package tools

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

func TestListDirDefinitionAndStrictArguments(t *testing.T) {
	catalog := NewCatalog()
	definition, ok := catalog.Definition(domainsecurity.ToolListDir)
	if !ok || definition.Name != domainsecurity.ToolListDir || !json.Valid(definition.InputSchema) {
		t.Fatalf("invalid list_dir definition: %#v", definition)
	}
	root := t.TempDir()
	invocation := runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
		WorkspacePathSnapshot: root, ReadScopes: []string{root},
	}}
	for _, input := range []string{
		`{"path":".","limit":0}`,
		`{"path":".","offset":null}`,
		`{"path":".","unknown":true}`,
		`{"path":".","path":"src"}`,
		`{"path":"."} {"path":"."}`,
	} {
		_, err := catalog.Normalize(runtimecontract.ToolCall{
			Name: domainsecurity.ToolListDir, Input: json.RawMessage(input),
		}, invocation)
		if err == nil {
			t.Fatalf("Normalize(%s) succeeded, want strict validation error", input)
		}
	}
	call, err := catalog.Normalize(runtimecontract.ToolCall{
		Name: domainsecurity.ToolListDir, Input: json.RawMessage(`{"path":"."}`),
	}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	var normalized listDirArguments
	if err := json.Unmarshal(call.NormalizedArguments, &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Path != filepath.Clean(root) || normalized.Offset != 0 || normalized.Limit != defaultListDirLimit {
		t.Fatalf("unexpected normalized arguments: %#v", normalized)
	}
}

func TestListDirReturnsSortedPaginatedEntries(t *testing.T) {
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
	call := normalizeListDir(t, root, `{"path":".","offset":1,"limit":1}`)
	result, err := NewExecutor().Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	var output listDirOutput
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatal(err)
	}
	if output.Total != 3 || len(output.Entries) != 1 ||
		output.Entries[0].Name != "middle.txt" || output.Entries[0].Type != "file" {
		t.Fatalf("unexpected list_dir output: %#v", output)
	}
	if output.NextOffset == nil || *output.NextOffset != 2 || !result.Truncated {
		t.Fatalf("missing pagination metadata: %#v", output)
	}
}

func TestListDirRejectsScopeAndSymlinkEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor()
	outsideCall := normalizeListDir(t, root, `{"path":"../outside"}`)
	if _, err := executor.Execute(context.Background(), outsideCall); err == nil {
		t.Fatal("scope escape succeeded")
	}
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	linkCall := normalizeListDir(t, root, `{"path":"outside-link"}`)
	if _, err := executor.Execute(context.Background(), linkCall); err == nil {
		t.Fatal("symlink escape succeeded")
	}
}

func TestListDirBoundsLargeOutput(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 400; index++ {
		name := fmt.Sprintf("%03d_%s", index, strings.Repeat("x", 150))
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	call := normalizeListDir(t, root, `{"path":".","limit":1000}`)
	result, err := NewExecutor().Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(result.Content)) > domainexecution.MaxInlineToolResultBytes {
		t.Fatalf("result size=%d exceeds limit", len([]byte(result.Content)))
	}
	var output listDirOutput
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || output.NextOffset == nil || len(output.Entries) >= output.Total {
		t.Fatalf("large output was not paginated: entries=%d total=%d", len(output.Entries), output.Total)
	}
}

func normalizeListDir(t *testing.T, root, input string) runtimecontract.AuthorizedToolCall {
	t.Helper()
	call, err := NewCatalog().Normalize(
		runtimecontract.ToolCall{Name: domainsecurity.ToolListDir, Input: json.RawMessage(input)},
		runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
			WorkspacePathSnapshot: root, ReadScopes: []string{root},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return call
}
