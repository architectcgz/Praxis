package readfile

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

// Definition returns the model-visible definition of the read_file tool.
func Definition() runtimecontract.ToolDefinition {
	return runtimecontract.ToolDefinition{
		Name: domainsecurity.ToolReadFile,
		Description: "Read a UTF-8 regular file. Returns JSON content with byte-offset pagination; " +
			"output is limited to 50 KiB.",
		InputSchema: append(json.RawMessage(nil), schemaJSON...),
	}
}
