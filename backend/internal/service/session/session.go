package session

import (
	"context"
	"errors"
	"fmt"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"slices"
)

// Session 将短读写交给单个接纳协程；完整数据只由 data 持有，没有另一份历史投影。
// 页面与执行可并发提交操作，模型请求、工具执行和 Hook 均不在接纳协程内运行。
type Session struct {
	config         *Configuration
	modelSelection sessionmodel.ModelSelection
	data           *sessionmodel.Data
	log            repository.SessionLog
	operations     chan func()
	stop           chan struct{}
	done           chan struct{}
	fault          error
	failed         chan struct{}
}

func (s *Session) serve() {
	defer close(s.done)
	for {
		select {
		case operation := <-s.operations:
			operation()
		case <-s.stop:
			return
		}
	}
}

// use 在接纳前允许取消；接纳后等待提交结果，调用方不能把已提交操作当成未执行。
func (s *Session) use(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result := make(chan error, 1)
	select {
	case s.operations <- func() {
		if s.fault != nil {
			result <- s.fault
			return
		}
		result <- operation()
	}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return errors.New("session is closed")
	}
	select {
	case err := <-result:
		return err
	case <-s.done:
		return errors.New("session is closed")
	}
}

func (s *Session) close() {
	close(s.stop)
	<-s.done
}

func (s *Service) openedSession(ctx context.Context, id identity.SessionID) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.openedMu.Lock()
	owner := s.opened[id]
	s.openedMu.Unlock()
	if owner == nil {
		return nil, errors.New("session is not open")
	}
	return owner, nil
}

// openSession 在发布引用之前完成加载和中断收尾，不在普通追加路径隐式恢复。
func (s *Service) openSession(ctx context.Context, id identity.SessionID) (*Session, error) {
	s.openedMu.Lock()
	defer s.openedMu.Unlock()
	if owner := s.opened[id]; owner != nil {
		return owner, nil
	}
	info, err := s.sessions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	config, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	config.ApprovalMode = security.EffectiveApprovalMode(config.GlobalApprovalMode, info.ApprovalMode)
	owner := &Session{
		config:     config,
		data:       sessionmodel.Open(info.ID),
		log:        s.log,
		operations: make(chan func()),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		failed:     make(chan struct{}),
	}
	defer func() {
		// 不确定写入后保留故障对象，后续操作只能返回故障，直到应用重启。
		if owner.fault != nil {
			s.opened[id] = owner
			go owner.serve()
		}
	}()
	replay := logReplay{session: owner.data, callIndexes: make(map[sessionentry.Key]int)}
	if err := owner.log.Read(ctx, id, replay.apply); err != nil {
		return nil, err
	}
	if len(owner.data.Branches) == 0 {
		branch := sessionmodel.Branch{
			ID:        identity.BranchID(s.ids.New("branch")),
			Name:      "main",
			RequestID: "initial",
			CreatedAt: s.clock.Now(),
			UpdatedAt: s.clock.Now(),
		}
		if err := owner.createBranch(ctx, branch); err != nil {
			return nil, err
		}
	}
	if err := replay.repairToolResults(ctx, owner, info.State); err != nil {
		return nil, err
	}
	for _, record := range slices.Clone(owner.data.Executions) {
		if !record.Active() {
			continue
		}
		at := s.clock.Now()
		if record.Status == agentexecution.ExecutionStarting {
			record.Start(at)
		}
		record.End(agentexecution.ExecutionInterrupted, "", at)
		if err := owner.saveExecution(ctx, record); err != nil {
			return nil, err
		}
	}
	s.opened[id] = owner
	go owner.serve()
	return owner, nil
}

func (s *Session) appendEvent(ctx context.Context, typ, collection, key string, value any) error {
	if s.fault != nil {
		return s.fault
	}
	event, err := newEvent(typ, collection, key, value)
	if err != nil {
		return err
	}
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	if err := s.log.Append(ctx, s.data.ID, data); err != nil {
		if errors.Is(err, errorcode.ErrLogUnavailable) {
			s.fault = fmt.Errorf("session log is unavailable until application restart: %w", err)
			close(s.failed)
		}
		return err
	}
	return nil
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}

func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}

func (s *Session) Faulted() <-chan struct{} { return s.failed }

func (s *Service) CheckLog(ctx context.Context, id identity.SessionID) error {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return err
	}
	return owner.use(ctx, func() error { return nil })
}
