package agentruntime

import "praxis/internal/core/domain"

// SessionManifest establishes the immutable identity and recovery rules for one
// AgentThread transcript. Run-specific inputs deliberately belong to RunInputs.
type SessionManifest struct {
	TaskSessionID    domain.TaskSessionID
	AgentThreadID    domain.AgentThreadID
	Profile          domain.AgentProfile
	WorkspaceKey     string
	InjectionNonce   string
	MinReaderVersion uint16
	WrittenBy        string
}

// RunInputs records the approved inputs actually used to start one AgentRun.
// It is emitted with run_started so later configuration changes cannot rewrite
// the audit boundary of an earlier run.
type RunInputs struct {
	TaskPacketID         domain.TaskPacketID
	ContextManifestID    domain.ContextManifestID
	GrantID              domain.CapabilityGrantID
	Execution            domain.RuntimeExecutionSnapshot
	Model                domain.ModelRef
	SystemPromptHash     string
	ToolDefinitionHashes map[string]string
	ArtifactTemplateHash string
}

// Snapshot returns a defensive copy of the mutable fingerprint map.
func (i RunInputs) Snapshot() RunInputs {
	copy := i
	copy.Execution = i.Execution.Snapshot()
	copy.ToolDefinitionHashes = cloneStringMap(i.ToolDefinitionHashes)
	return copy
}

func (r *Runtime) sessionManifest() SessionManifest {
	return SessionManifest{
		TaskSessionID:    r.config.TaskSessionID,
		AgentThreadID:    r.config.AgentThreadID,
		Profile:          r.config.Thread.Profile,
		WorkspaceKey:     r.config.Grant.WorkspaceKey,
		InjectionNonce:   r.config.InjectionNonce,
		MinReaderVersion: r.config.MinimumReaderVersion,
		WrittenBy:        r.config.WrittenBy,
	}
}

func (r *Runtime) runInputs(run domain.AgentRun) RunInputs {
	return RunInputs{
		TaskPacketID:         r.config.TaskPacket.ID,
		ContextManifestID:    r.config.ContextManifest.ID,
		GrantID:              r.config.Grant.ID,
		Execution:            run.Execution.Snapshot(),
		Model:                r.config.Model,
		SystemPromptHash:     r.config.Prompt.SystemPromptHash,
		ToolDefinitionHashes: cloneStringMap(r.config.ToolDefinitionHashes),
		ArtifactTemplateHash: r.config.Prompt.ArtifactTemplateHash,
	}
}
