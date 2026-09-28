package contracts

import "strings"

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
		return "", InvalidValue("profile", "unknown agent profile")
	}
	return profile, nil
}

// Valid 判断定义 ID 是否可安全用作本地目录名。
func (id AgentDefinitionID) Valid() bool {
	raw := string(id)
	if raw == "" || len(raw) > 64 || raw == "." || raw == ".." {
		return false
	}
	for _, char := range raw {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return !strings.HasPrefix(raw, "-") && !strings.HasPrefix(raw, "_")
}
