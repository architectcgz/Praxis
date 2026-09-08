package tools

import (
	"encoding/json"
	"testing"

	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

func TestToolRegistryRegistersReadTools(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []domainsecurity.ToolName{domainsecurity.ToolReadFile, domainsecurity.ToolListDir} {
		definition, ok := registry.Definition(name)
		if !ok || definition.Name != name || !json.Valid(definition.InputSchema) {
			t.Fatalf("invalid %s definition: %#v", name, definition)
		}
	}
	root := t.TempDir()
	for _, name := range []domainsecurity.ToolName{domainsecurity.ToolReadFile, domainsecurity.ToolListDir} {
		call, err := registry.Normalize(
			runtimecontract.ToolCall{Name: name, Input: json.RawMessage(`{"path":"."}`)},
			runtimecontract.ToolInvocationContext{Grant: domainsecurity.CapabilityGrant{
				WorkspacePathSnapshot: root, ReadScopes: []string{root},
			}},
		)
		if err != nil {
			t.Fatal(err)
		}
		if call.Name != name || call.Path != root || !json.Valid(call.NormalizedArguments) {
			t.Fatalf("unexpected normalized call: %#v", call)
		}
	}
}
