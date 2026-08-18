package domain

type AgentProfile string

const (
	ProfilePrimary  AgentProfile = "primary"
	ProfileDelegate AgentProfile = "delegate"
	ProfileConsult  AgentProfile = "consult"
	ProfileNote     AgentProfile = "note"
)

func (p AgentProfile) Valid() bool {
	switch p {
	case ProfilePrimary, ProfileDelegate, ProfileConsult, ProfileNote:
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
