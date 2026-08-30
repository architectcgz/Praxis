package domain

// RuntimeExecutionSnapshot freezes the technical sandbox and approval behavior
// for one AgentExecution so policy changes cannot alter an in-flight side effect.
type RuntimeExecutionSnapshot struct {
	SandboxMode  SandboxMode
	ApprovalMode ApprovalMode
	Revision     string
}

func NewRuntimeExecutionSnapshot(
	sandboxMode SandboxMode,
	approvalMode ApprovalMode,
	revision string,
) (RuntimeExecutionSnapshot, error) {
	snapshot := RuntimeExecutionSnapshot{
		SandboxMode:  sandboxMode,
		ApprovalMode: approvalMode,
		Revision:     revision,
	}
	if err := snapshot.Validate(); err != nil {
		return RuntimeExecutionSnapshot{}, err
	}
	return snapshot, nil
}

func (s RuntimeExecutionSnapshot) Validate() error {
	if !s.SandboxMode.Valid() {
		return invalidValue("runtimeExecution.sandboxMode", "unknown sandbox mode")
	}
	if !s.ApprovalMode.Valid() {
		return invalidValue("runtimeExecution.approvalMode", "unknown approval mode")
	}
	return nil
}

func (s RuntimeExecutionSnapshot) Snapshot() RuntimeExecutionSnapshot { return s }
