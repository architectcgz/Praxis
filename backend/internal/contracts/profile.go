package contracts

type AgentProfile string

const (
	ProfilePrimary  AgentProfile = "primary"
	ProfileDelegate AgentProfile = "delegate"
	ProfileAdvisor  AgentProfile = "advisor"
	ProfileCurator  AgentProfile = "curator"
)
