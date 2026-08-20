package domain

type AgentPolicySnapshot struct {
	Revision     string
	ApprovalMode ApprovalMode
	SandboxMode  SandboxMode
	Profiles     map[AgentProfile]DefaultGrantTemplate
}

func NewAgentPolicySnapshot(
	revision string,
	mode ApprovalMode,
	sandboxMode SandboxMode,
	profiles map[AgentProfile]DefaultGrantTemplate,
) (AgentPolicySnapshot, error) {
	snapshot := AgentPolicySnapshot{
		Revision:     revision,
		ApprovalMode: mode,
		SandboxMode:  sandboxMode,
		Profiles:     cloneTemplates(profiles),
	}
	if err := snapshot.Validate(); err != nil {
		return AgentPolicySnapshot{}, err
	}
	return snapshot, nil
}

func (p AgentPolicySnapshot) Validate() error {
	if !p.ApprovalMode.Valid() {
		return invalidValue("agentPolicy.approvalMode", "unknown approval mode")
	}
	if !p.SandboxMode.Valid() {
		return invalidValue("agentPolicy.sandboxMode", "unknown sandbox mode")
	}
	for profile, template := range p.Profiles {
		if !profile.Valid() {
			return invalidValue("agentPolicy.profiles", "unknown agent profile")
		}
		if err := template.Validate(); err != nil {
			return fmtField("agentPolicy.profiles", err)
		}
	}
	return nil
}

func (p AgentPolicySnapshot) TemplateFor(profile AgentProfile) (DefaultGrantTemplate, bool) {
	template, ok := p.Profiles[profile]
	if !ok {
		return DefaultGrantTemplate{}, false
	}
	return template.Snapshot(), true
}

func (p AgentPolicySnapshot) Snapshot() AgentPolicySnapshot {
	return AgentPolicySnapshot{
		Revision:     p.Revision,
		ApprovalMode: p.ApprovalMode,
		SandboxMode:  p.SandboxMode,
		Profiles:     cloneTemplates(p.Profiles),
	}
}

func cloneTemplates(values map[AgentProfile]DefaultGrantTemplate) map[AgentProfile]DefaultGrantTemplate {
	if values == nil {
		return nil
	}
	result := make(map[AgentProfile]DefaultGrantTemplate, len(values))
	for profile, template := range values {
		result[profile] = template.Snapshot()
	}
	return result
}
