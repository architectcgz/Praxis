package run

import (
	"praxis/internal/identity"
	"time"
)

// TurnRecord 保存一次实际模型请求的区间；普通请求、摘要和审核使用相同序号空间。
// 首轮在输入准入时建立，请求尚未发出时 RequestID 为空。
type TurnRecord struct {
	ID        identity.TurnID
	RunID     identity.RunID
	Sequence  uint64
	RequestID identity.ModelRequestID
	StartedAt time.Time
	EndedAt   time.Time
}

func newTurn(runID identity.RunID, sequence uint64, at time.Time) TurnRecord {
	return TurnRecord{
		ID:        identity.NewTurnID(runID, sequence),
		RunID:     runID,
		Sequence:  sequence,
		StartedAt: at.UTC(),
	}
}

// BeginTurn 复用尚未发出请求的首轮；下一次实际请求推进序号。
func (r *RunRecord) BeginTurn(at time.Time) {
	if r.CurrentTurn.RequestID == "" && r.CurrentTurn.EndedAt.IsZero() {
		return
	}
	r.CurrentTurn = newTurn(r.ID, r.CurrentTurn.Sequence+1, at)
}

func (r *RunRecord) EndTurn(at time.Time) {
	if r.CurrentTurn.EndedAt.IsZero() {
		r.CurrentTurn.EndedAt = at.UTC()
	}
}
