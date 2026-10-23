package storage

import (
	"context"
	"fmt"
	"praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	"reflect"
)

type ToolExecutionRepository struct{ s *Store }

func (r ToolExecutionRepository) GetApprovalRequest(ctx context.Context, key toolexecution.Key) (value toolexecution.ApprovalRequest, found bool, err error) {
	err = r.s.view(ctx, func() error {
		event, ok, readErr := r.latestLocked(key, toolexecution.ApprovalRequired)
		if readErr == nil && ok {
			value, found = *event.Request, true
		}
		return readErr
	})
	return
}

func (r ToolExecutionRepository) GetUserDecision(ctx context.Context, key toolexecution.Key) (value toolexecution.UserDecision, found bool, err error) {
	err = r.s.view(ctx, func() error {
		event, ok, readErr := r.latestLocked(key, toolexecution.ApprovalDecided)
		if readErr == nil && ok {
			value, found = *event.UserDecision, true
		}
		return readErr
	})
	return
}

func (r ToolExecutionRepository) GetReview(ctx context.Context, key toolexecution.Key) (value toolexecution.ReviewRecord, found bool, err error) {
	err = r.s.view(ctx, func() error {
		event, ok, readErr := r.latestLocked(key, toolexecution.Reviewed)
		if readErr == nil && ok {
			value, found = *event.Review, true
		}
		return readErr
	})
	return
}

// latestLocked 只按序号定位所需事实并读取正文，不构建审批聚合投影。
func (r ToolExecutionRepository) latestLocked(key toolexecution.Key, kind toolexecution.EventKind) (toolexecution.Event, bool, error) {
	keys := r.s.keys(lookupKey{kind: "tool-event-kind", owner: key.String(), value: string(kind)})
	var latest entityKey
	var sequence uint64
	for _, candidate := range keys {
		entry, _ := r.s.entry(candidate)
		if entry.eventSequence > sequence {
			latest, sequence = candidate, entry.eventSequence
		}
	}
	if sequence == 0 {
		return toolexecution.Event{}, false, nil
	}
	data, err := r.s.payload(latest)
	if err != nil {
		return toolexecution.Event{}, false, err
	}
	var event toolexecution.Event
	err = decodeJSON(data, &event)
	return event, err == nil, err
}

// SaveAuthorization 在单次存储锁内验证决定幂等性，矛盾决定不能覆盖已提交事实。
func (r ToolExecutionRepository) SaveAuthorization(ctx context.Context, event toolexecution.Event) error {
	return r.s.view(ctx, func() error {
		if err := validateToolEvent(event); err != nil {
			return err
		}
		if event.Kind == toolexecution.ApprovalDecided {
			previous, found, err := r.latestLocked(event.Key, toolexecution.ApprovalDecided)
			if err != nil {
				return err
			}
			if found {
				if previous.UserDecision.Approved != event.UserDecision.Approved {
					return errorcode.ErrRequestConflict
				}
				return nil
			}
		}
		if event.Kind == toolexecution.ApprovalRequired {
			previous, found, err := r.latestLocked(event.Key, event.Kind)
			if err != nil {
				return err
			}
			if found {
				if reflect.DeepEqual(previous.Request, event.Request) {
					return nil
				}
				return errorcode.ErrRequestConflict
			}
		}
		sequence := r.s.log.Sequence(event.SessionID.String()) + 1
		return r.s.appendLocked(ctx, event.SessionID.String(), "tool_event", fmt.Sprintf("%s@%d", event.Key, sequence), string(event.Kind), event)
	})
}

// validateAuthorizationLocked 在写入与重放时共同检查事实引用，不派生执行状态。
func (r ToolExecutionRepository) validateAuthorizationLocked(event toolexecution.Event) error {
	switch event.Kind {
	case toolexecution.ApprovalRequired:
		_, found, err := r.latestLocked(event.Key, event.Kind)
		if err != nil {
			return err
		}
		if found {
			return errorcode.ErrRequestConflict
		}
	case toolexecution.ApprovalDecided:
		request, found, err := r.latestLocked(event.Key, toolexecution.ApprovalRequired)
		if err != nil {
			return err
		}
		if !found || event.At.Before(request.At) {
			return errorcode.ErrRequestConflict
		}
		_, found, err = r.latestLocked(event.Key, event.Kind)
		if err != nil {
			return err
		}
		if found {
			return errorcode.ErrRequestConflict
		}
	}
	return nil
}
