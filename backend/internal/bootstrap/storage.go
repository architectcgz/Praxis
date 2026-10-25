package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/dataroot"
	"praxis/internal/infra/storage"
	"praxis/internal/logging"
	"praxis/internal/repository"
	"time"
)

type storageResources struct {
	storage *storage.Storage
	logger  *logging.Logger
}

func openStorage(ctx context.Context, root dataroot.DataRoot) (storageResources, error) {
	logger, err := logging.NewFactory().Application(filepath.Join(root.State, "praxis.log"))
	if err != nil {
		return storageResources{}, fmt.Errorf("open runtime log: %w", err)
	}
	logger.Infof("application open started root=%s logs=%s", root.Root, root.State)
	state, err := storage.Open(ctx, root.Root, func(err error) {
		logger.Errorf("storage persistence degraded; runtime continues in memory: %v", err)
	})
	if err != nil {
		logger.Errorf("open storage failed: %v", err)
		_ = logger.Close()
		return storageResources{}, fmt.Errorf("open storage: %w", err)
	}
	return storageResources{storage: state, logger: logger}, nil
}

func usageObserver(usages repository.ModelUsage, logger *logging.Logger) func(providerapi.UsageRecord) {
	return func(record providerapi.UsageRecord) {
		// 统计写入不继承业务取消；失败只影响统计，不改变模型响应。
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := usages.Save(ctx, record); err != nil {
			logger.Errorf("token usage save failed task=%s turn=%s: %v", record.ExecutionID, record.TurnID, err)
		}
	}
}
