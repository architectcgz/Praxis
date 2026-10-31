package storage

import (
	"context"
	"fmt"
	"praxis/internal/infra/jsonl"
)

// appendLogLocked 调用方须持有 Store 写锁；写入失败直接返回，不保留错误状态。
func (s *Store) appendLogLocked(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := jsonl.Append(path, data); err != nil {
		return fmt.Errorf("append log %s: %w", path, err)
	}
	return nil
}
