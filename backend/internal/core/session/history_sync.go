package session

import "context"

// syncHistory 由提交边界调用。追加曾报告失败但后来确认完整落盘的不可变 Entry，
// 在索引补齐后释放保留引用；缺少完整记录的接纳结果继续由会话持有。
func (s *Session) syncHistory(ctx context.Context) error {
	if err := s.log.Sync(ctx); err != nil {
		return err
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.data.PendingEntries))
	for id := range s.data.PendingEntries {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		return nil
	}
	nodes, err := s.log.IndexedNodes(ctx, ids)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, node := range nodes {
		s.data.releaseEntry(node.ID)
	}
	s.mu.Unlock()
	return nil
}
