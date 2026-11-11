package run

import (
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
	"time"
)

// HistorySummary 是可重建的执行目录，不包含冻结配置或历史正文。
// 完整 RunRecord 仅在执行和必要恢复中读取，展示与累计使用该投影。
type HistorySummary struct {
	Snapshot
	SessionID     identity.SessionID
	BranchID      identity.BranchID
	AgentID       identity.AgentID
	AgentName     string
	DefinitionID  identity.AgentDefinitionID
	ParentRunID   identity.RunID
	SourceTaskID  identity.TaskID
	InputEntryID  string
	CreatedAt     time.Time
	Usage         modelusage.Totals
	ModelRequests uint64
}

func (r RunRecord) HistorySummary() HistorySummary {
	return HistorySummary{
		Snapshot:      r.Snapshot(),
		SessionID:     r.SessionID,
		BranchID:      r.BranchID,
		AgentID:       r.AgentID,
		AgentName:     r.AgentName,
		DefinitionID:  r.Config.AgentDefinitionID,
		ParentRunID:   r.ParentRunID,
		SourceTaskID:  r.SourceTaskID,
		InputEntryID:  r.InputEntryID,
		CreatedAt:     r.CreatedAt,
		Usage:         r.Progress.Usage,
		ModelRequests: r.Progress.ModelRequests,
	}
}
