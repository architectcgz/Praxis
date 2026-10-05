package contracts

// SandboxMode 固定命令的文件和网络边界，由 Turn 的 SecuritySnapshot 保存。
type SandboxMode string

const (
	SandboxReadOnly         SandboxMode = "read_only"
	SandboxWorkspaceWrite   SandboxMode = "workspace_write"
	SandboxWorkspaceNetwork SandboxMode = "workspace_network"
)

// Valid 判断当前 Sandbox 模式是否受支持。
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
