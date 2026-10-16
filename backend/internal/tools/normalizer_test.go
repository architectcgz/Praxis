package tools

import (
	"encoding/json"
	"path/filepath"
	"praxis/internal/contracts"
	"testing"
)

func TestNormalizersResolveWorkspaceBeforeAdmission(t *testing.T) {
	registry := NewToolRegistry()
	root := t.TempDir()
	for _, tc := range []struct {
		name      contracts.ToolName
		arguments string
		relative  string
	}{
		{contracts.ToolReadFile, `{"path":"sub/../file"}`, "file"},
		{contracts.ToolBash, `{"command":"pwd","path":"sub/.."}`, "."},
		{contracts.ToolApplyPatch, "{\"patch\":\"*** Begin Patch\\n*** Add File: file\\n+text\\n*** End Patch\"}", "."},
	} {
		t.Run(string(tc.name), func(t *testing.T) {
			tool, ok := registry.Get(tc.name)
			if !ok {
				t.Fatal(tc.name)
			}
			value, err := tool.Normalize(contracts.ToolCall{ID: "call", Name: tc.name, Arguments: json.RawMessage(tc.arguments)}, root)
			if err != nil {
				t.Fatal(err)
			}
			var arguments struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(value.NormalizedArguments, &arguments); err != nil {
				t.Fatal(err)
			}
			if value.Path != filepath.Join(root, tc.relative) || arguments.Path != value.Path {
				t.Fatal(value, arguments)
			}
		})
	}
}
