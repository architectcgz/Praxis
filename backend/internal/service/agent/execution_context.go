package agent

import (
	"slices"
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

// Clone 隔离已冻结的历史选择规则。
func (s ContextSelection) Clone() ContextSelection {
	s.ExcludedMessageIDs = slices.Clone(s.ExcludedMessageIDs)
	s.SessionContextRefs = slices.Clone(s.SessionContextRefs)
	return s
}
