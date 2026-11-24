package storage

import (
	"context"
)

// Store 提供各存储入口并管理数据库生命周期，不缓存会话历史。
type Store struct {
	Projects ProjectRepository
	Sessions SessionRepository
}

// Open 使用装配层已初始化的目录打开会话元数据库，不加载会话历史。
func Open(ctx context.Context, dataRoot, projectsRoot string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := openSessionMetadataDatabase(ctx, dataRoot)
	if err != nil {
		return nil, err
	}
	sessions := SessionRepository{
		backupDir:    dataRoot,
		projectsRoot: projectsRoot,
		database:     db,
	}
	return &Store{
		Projects: ProjectRepository{root: projectsRoot},
		Sessions: sessions,
	}, nil
}

func (s *Store) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Sessions.database.Close()
}
