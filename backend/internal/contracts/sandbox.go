package contracts

// SandboxMode fixes the filesystem and network boundary for command
// It is copied into a runtime execution snapshot and cannot be changed by a running agent.
type SandboxMode string

const (
	SandboxReadOnly         SandboxMode = "read_only"
	SandboxWorkspaceWrite   SandboxMode = "workspace_write"
	SandboxWorkspaceNetwork SandboxMode = "workspace_network"
)

// Valid reports whether the mode is supported by the P1 execution policy.
func (m SandboxMode) Valid() bool {
	switch m {
	case SandboxReadOnly, SandboxWorkspaceWrite, SandboxWorkspaceNetwork:
		return true
	default:
		return false
	}
}

// AllowsWorkspaceWrite reports whether commands may write inside the workspace boundary.
func (m SandboxMode) AllowsWorkspaceWrite() bool {
	return m == SandboxWorkspaceWrite || m == SandboxWorkspaceNetwork
}

// AllowsNetwork reports whether commands may use the sandboxed network boundary.
func (m SandboxMode) AllowsNetwork() bool {
	return m == SandboxWorkspaceNetwork
}
