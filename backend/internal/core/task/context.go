package task

import (
	"praxis/internal/contracts"
	"slices"
	"strings"
)

// ContextSelection 保存启动时冻结的历史边界、排除项和共享事实版本。
type ContextSelection struct {
	MessageSequenceBoundary uint64 `json:"messageSequenceBoundary"`
	ExcludedMessageIDs      []string
	SessionContextRefs      []ContextReference
}

type ContextReference struct {
	ID     string
	Digest string
}

func (s ContextSelection) Validate() error {
	for _, id := range s.ExcludedMessageIDs {
		if id == "" || id != strings.TrimSpace(id) {
			return contracts.InvalidValue("task.contextSelection.excludedMessageIDs", "message id must be canonical and nonempty")
		}
	}
	for _, ref := range s.SessionContextRefs {
		if ref.ID == "" || ref.ID != strings.TrimSpace(ref.ID) || ref.Digest == "" || ref.Digest != strings.TrimSpace(ref.Digest) {
			return contracts.InvalidValue("task.contextSelection.sessionContextRefs", "context reference must be canonical and nonempty")
		}
	}
	return nil
}

// Clone 隔离已冻结的历史选择规则。
func (s ContextSelection) Clone() ContextSelection {
	s.ExcludedMessageIDs = slices.Clone(s.ExcludedMessageIDs)
	s.SessionContextRefs = slices.Clone(s.SessionContextRefs)
	return s
}
