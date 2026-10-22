package hook

import (
	"context"
	"praxis/internal/identity"
)

// UserInput 尚未入队，不存在 Execution；处理器只返回正文，不能更改目标 Session。
type UserInput struct {
	SessionID identity.SessionID
	Prompt    string
}

type BeforeUserInputHook interface {
	BeforeUserInput(context.Context, UserInput) (string, error)
}
