package service

import (
	"context"
	"errors"
	"praxis/internal/agent"
	"praxis/internal/clock"
	"praxis/internal/fault"
	"praxis/internal/hook"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/message"
	"praxis/internal/task"
	"praxis/internal/transaction"
)

type Dependencies struct {
	Transactions   transaction.Runner
	Tasks          task.Store
	Agents         agent.Store
	Messages       message.Streams
	HookDispatcher *hook.Dispatcher
	Logger         *logging.Logger
	Clock          clock.Clock
}

// Service 管理预约的幂等提交、FIFO 查询与取消，不运行模型或结算执行。
type Service struct {
	tx             transaction.Runner
	tasks          task.Store
	agents         agent.Store
	messages       message.Streams
	hookDispatcher *hook.Dispatcher
	logger         *logging.Logger
	clock          clock.Clock
}

func NewService(d Dependencies) (*Service, error) {
	if d.Transactions == nil || d.Tasks == nil || d.Agents == nil || d.Messages == nil {
		return nil, errors.New("queue dependencies are required")
	}
	return &Service{
		tx:             d.Transactions,
		tasks:          d.Tasks,
		agents:         d.Agents,
		messages:       d.Messages,
		hookDispatcher: d.HookDispatcher,
		logger:         logging.NewFactory().Ensure(d.Logger),
		clock:          clock.ClockOrDefault(d.Clock),
	}, nil
}

func (s *Service) inputMessageContent(ctx context.Context, owner agent.Agent, requestID identity.RequestID) (string, error) {
	messages, err := s.messages.List(ctx, owner.SessionID, owner.ID, 0, 0)
	if err != nil {
		return "", err
	}
	for _, m := range messages {
		if m.ID == "input:"+requestID.String() && m.Role == message.RoleUser {
			return m.TextContent(), nil
		}
	}
	return "", fault.ErrNotFound
}

func (s *Service) Cancel(ctx context.Context, id identity.TaskID) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		value, err := s.tasks.Get(ctx, id)
		if err != nil {
			return err
		}
		if value.Status == task.Cancelled {
			return nil
		}
		if err := value.Cancel(s.clock.Now()); err != nil {
			return err
		}
		return s.tasks.Save(ctx, value)
	})
}
