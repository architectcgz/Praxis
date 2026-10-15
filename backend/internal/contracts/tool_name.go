package contracts

import (
	"strings"
)

type ToolName string

const (
	ToolReadFile   ToolName = "read_file"
	ToolBash       ToolName = "bash"
	ToolApplyPatch ToolName = "apply_patch"
)

func (t ToolName) Valid() bool {
	if t == "" || len(t) > 64 || strings.HasPrefix(string(t), "_") {
		return false
	}
	for _, char := range t {
		if char != '_' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}
