package compose

import (
	"path/filepath"

	domainexecution "praxis/internal/core/domain/execution"

	"praxis/internal/logging"
	"praxis/internal/storage/dataroot"
)

type runtimeLog struct {
	logger *logging.Logger
}

func openRuntimeLog(root dataroot.DataRoot) (*runtimeLog, error) {
	path := filepath.Join(root.Runtime, "praxis.log")
	logger, err := logging.NewFactory().Runtime(path)
	if err != nil {
		return nil, err
	}
	return &runtimeLog{logger: logger}, nil
}

func (l *runtimeLog) Log(execution domainexecution.AgentExecution, stage string, err error) {
	if l == nil || err == nil {
		return
	}
	l.logger.Errorf("execution id=%s stage=%s failed: %v", execution.ID, stage, err)
}

func (l *runtimeLog) Event(execution domainexecution.AgentExecution, stage string) {
	if l == nil {
		return
	}
	l.logger.Infof("execution id=%s stage=%s", execution.ID, stage)
}

func (l *runtimeLog) Logger() *logging.Logger {
	if l == nil {
		return nil
	}
	return l.logger
}

func (l *runtimeLog) Close() error {
	if l == nil {
		return nil
	}
	return l.logger.Close()
}
