package session

import (
	"context"
	"errors"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"strings"
)

type TaskInput struct {
	BindingID    string
	SessionID    identity.SessionID
	BranchID     identity.BranchID
	SubmissionID identity.TaskID
	Prompt       string
}

// CreateTask 在入队入口执行输入 Hook；队列归页面 Session，运行期间不修改 Context。
func (s *Service) CreateTask(ctx context.Context, input TaskInput) (sessionmodel.Task, error) {
	prompt, err := s.hookDispatcher.BeforeUserInput(ctx, hook.UserInput{SessionID: input.SessionID, Prompt: input.Prompt})
	if err != nil {
		return sessionmodel.Task{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return sessionmodel.Task{}, errors.New("task prompt is required")
	}
	if err := RequireWritable(ctx, s.sessions, input.SessionID); err != nil {
		return sessionmodel.Task{}, err
	}
	value := sessionmodel.NewTask(input.SubmissionID, input.SessionID, prompt, s.clock.Now())
	value.BranchID = input.BranchID
	return value, nil
}
