package listdir

import (
	_ "embed"
	"encoding/json"

	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

// schemaJSON is kept as a separate asset because it is a model-facing protocol
// definition and should evolve independently from Go implementation code.
//
//go:embed schema.json
var schemaJSON []byte

// Definition returns the model-visible definition of the list_dir tool.
func Definition() runtimecontract.ToolDefinition {
	return runtimecontract.ToolDefinition{
		Name: domainsecurity.ToolListDir,
		Description: "List one directory without recursion. Returns sorted JSON entries with type and " +
			"pagination metadata; output is limited to 50 KiB.",
		InputSchema: append(json.RawMessage(nil), schemaJSON...),
	}
}
