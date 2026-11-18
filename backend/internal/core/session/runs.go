package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// FindTaskInputID 只定位已接纳任务的输入，不重建执行对象。
func (s *Session) FindTaskInputID(ctx context.Context, taskID identity.TaskID) (string, error) {
	var inputID string
	err := s.walkHistory(ctx, s.data.MainAgentID(), func(value *sessionentry.Entry) bool {
		prepared, ok := value.Payload.(entryPayload.RunPrepared)
		if ok && prepared.SourceTaskID == taskID {
			inputID = prepared.InputEntryID
			return false
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if inputID == "" {
		return "", errorcode.ErrNotFound
	}
	return inputID, nil
}
