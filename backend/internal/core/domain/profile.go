package domain

type AgentProfile string

const (
	ProfilePrimary  AgentProfile = "primary"
	ProfileDelegate AgentProfile = "delegate"
	ProfileAdvisor  AgentProfile = "advisor"
	ProfileCurator  AgentProfile = "curator"
)

func (p AgentProfile) Valid() bool {
	switch p {
	case ProfilePrimary, ProfileDelegate, ProfileAdvisor, ProfileCurator:
		return true
	default:
		return false
	}
}

func ParseAgentProfile(raw string) (AgentProfile, error) {
	profile := AgentProfile(raw)
	if !profile.Valid() {
		return "", invalidValue("profile", "unknown agent profile")
	}
	return profile, nil
}
