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
