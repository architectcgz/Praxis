package dto

import sessionmodel "praxis/internal/core/session"

type HistoryPage struct {
	HeadEntryID string  `json:"headEntryId"`
	Entries     []Entry `json:"entries"`
	HasMore     bool    `json:"hasMore"`
	NextCursor  string  `json:"nextCursor"`
}

type EntryHistoryRequest struct {
	SessionID     string `json:"sessionId" validate:"session_id"`
	HeadEntryID   string `json:"headEntryId" validate:"omitempty,id"`
	BeforeEntryID string `json:"beforeEntryId" validate:"omitempty,id"`
	Limit         int    `json:"limit" validate:"min=1,max=200"`
}

type AgentHistoryRequest struct {
	SessionID     string `json:"sessionId" validate:"session_id"`
	BranchID      string `json:"branchId" validate:"id"`
	AgentID       string `json:"agentId" validate:"id"`
	HeadEntryID   string `json:"headEntryId" validate:"omitempty,id"`
	BeforeEntryID string `json:"beforeEntryId" validate:"omitempty,id"`
	Limit         int    `json:"limit" validate:"min=1,max=200"`
}

type DeleteBranchRequest struct {
	SessionID      string `json:"sessionId" validate:"session_id"`
	BranchID       string `json:"branchId" validate:"id"`
	TargetBranchID string `json:"targetBranchId" validate:"branch_id"`
	BindingID      string `json:"bindingId" validate:"omitempty,id"`
}

// DeleteBranchResponse 总是表达已提交的选择，清理错误不撤销逻辑删除。
type DeleteBranchResponse struct {
	Branch       sessionmodel.Branch `json:"branch"`
	BindingID    string              `json:"bindingId"`
	CleanupError string              `json:"cleanupError"`
}

type UsageRequest struct {
	SessionID string   `json:"sessionId" validate:"session_id"`
	TurnIDs   []string `json:"turnIds" validate:"max=2000,dive,id"`
}
