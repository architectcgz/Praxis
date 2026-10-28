package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"praxis/internal/dataroot"
	"praxis/internal/infra/storage"
	"praxis/internal/logging"
)

type storageResources struct {
	storage *storage.Store
	logger  *logging.Logger
}

func openStorage(ctx context.Context, root dataroot.DataRoot) (storageResources, error) {
	logger, err := logging.NewFactory().Application(filepath.Join(root.State, "praxis.log"))
	if err != nil {
		return storageResources{}, fmt.Errorf("open runtime log: %w", err)
	}
	logger.Infof("application open started root=%s logs=%s", root.Root, root.State)
	state, err := storage.Open(ctx, root.Root)
	if err != nil {
		logger.Errorf("open storage failed: %v", err)
		_ = logger.Close()
		return storageResources{}, fmt.Errorf("open storage: %w", err)
	}
	return storageResources{storage: state, logger: logger}, nil
}
