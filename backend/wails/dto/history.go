package dto

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
	SessionID string `json:"sessionId" validate:"session_id"`

	AgentID       string `json:"agentId" validate:"id"`
	HeadEntryID   string `json:"headEntryId" validate:"omitempty,id"`
	BeforeEntryID string `json:"beforeEntryId" validate:"omitempty,id"`
	Limit         int    `json:"limit" validate:"min=1,max=200"`
}

type UsageRequest struct {
	SessionID string   `json:"sessionId" validate:"session_id"`
	TurnIDs   []string `json:"turnIds" validate:"max=2000,dive,id"`
}
