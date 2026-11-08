package coordinator

import (
	"context"
	"praxis/internal/identity"
)

// sessionCommand 串行化同一会话的命令与调度登记，不覆盖模型、工具执行和运行结算。
// refs 包含持有者与等待者，归零后移除登记，不依赖已加载实例的存续时间。
type sessionCommand struct {
	held chan struct{}
	refs int
}

func (s *Coordinator) lockSession(ctx context.Context, id identity.SessionID) (func(), error) {
	s.mu.Lock()
	command := s.sessionCommands[id]
	if command == nil {
		command = &sessionCommand{held: make(chan struct{}, 1)}
		s.sessionCommands[id] = command
	}
	command.refs++
	s.mu.Unlock()

	select {
	case command.held <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-command.held
			s.releaseSessionCommand(id, command)
			return nil, err
		}
		return func() {
			<-command.held
			s.releaseSessionCommand(id, command)
		}, nil
	case <-ctx.Done():
		s.releaseSessionCommand(id, command)
		return nil, ctx.Err()
	}
}

func (s *Coordinator) releaseSessionCommand(id identity.SessionID, command *sessionCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command.refs--
	if command.refs == 0 {
		delete(s.sessionCommands, id)
	}
}
