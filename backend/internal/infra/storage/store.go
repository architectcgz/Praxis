package storage

import (
	"context"
	"os"
	"path/filepath"
)

// Store 提供各存储入口并管理数据库生命周期，不缓存会话历史。
type Store struct {
	Projects ProjectRepository
	Sessions SessionRepository
	Logs     SessionLog
}

// Open 初始化项目配置目录并打开会话元数据库，不加载会话历史。
func Open(ctx context.Context, dataRoot string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, "projects"), 0700); err != nil {
		return nil, err
	}
	db, err := openSessionMetadataDatabase(ctx, dataRoot)
	if err != nil {
		return nil, err
	}
	sessions := SessionRepository{
		dataRoot: dataRoot,
		database: db,
	}
	return &Store{
		Projects: ProjectRepository{dataRoot: dataRoot},
		Sessions: sessions,
		Logs:     SessionLog{sessions: sessions},
	}, nil
}

func (s *Store) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Sessions.database.Close()
}
