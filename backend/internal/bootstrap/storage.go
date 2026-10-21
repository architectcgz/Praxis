package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"praxis/internal/core/model"
	timingmodel "praxis/internal/core/timing"
	"praxis/internal/dataroot"
	"praxis/internal/identity"
	"praxis/internal/infra/storage"
	"praxis/internal/logging"
	"praxis/internal/repository"
	execution "praxis/internal/service/agent/coordinator"
	"praxis/internal/timing"
	"time"
)

type storageResources struct {
	store  *storage.Store
	logger *logging.Logger
}

func openStorage(ctx context.Context, root dataroot.DataRoot) (storageResources, error) {
	store, err := storage.Open(ctx, root.Root)
	if err != nil {
		return storageResources{}, fmt.Errorf("open storage: %w", err)
	}
	logger, err := logging.NewFactory().Application(filepath.Join(root.State, "praxis.log"))
	if err != nil {
		_ = store.Close(context.Background())
		return storageResources{}, fmt.Errorf("open runtime log: %w", err)
	}
	logger.Infof("application open started root=%s logs=%s", root.Root, root.State)
	return storageResources{store: store, logger: logger}, nil
}

func buildTimings(ctx context.Context, timings repository.Timing, events *execution.EventPublisher, logger *logging.Logger) (*timing.Recorder, error) {
	if err := timings.RecoverInterrupted(ctx); err != nil {
		return nil, fmt.Errorf("recover operation timings: %w", err)
	}
	return timing.New(timings, func(record timingmodel.Record) {
		events.Publish(execution.Event{
			Kind:        "operation_timing",
			AgentID:     identity.AgentID(record.AgentID),
			ExecutionID: identity.AgentExecutionID(record.ExecutionID),
			Timing:      &record,
		})
	}, logger.Errorf)
}

func usageObserver(usages repository.ModelUsage, logger *logging.Logger) func(model.ModelUsageRecord) {
	return func(record model.ModelUsageRecord) {
		// 统计写入不继承业务取消；失败只影响统计，不改变模型响应。
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := usages.Save(ctx, record); err != nil {
			logger.Errorf("token usage save failed task=%s turn=%s: %v", record.ExecutionID, record.TurnID, err)
		}
	}
}
