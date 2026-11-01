package dto

type CreateBranchRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	BranchID  string `json:"branchId" validate:"id"`
	BindingID string `json:"bindingId" validate:"omitempty,id"`
	RequestID string `json:"requestId" validate:"id"`
	Name      string `json:"name" validate:"omitempty,notblank,normalized,no_control"`
	EntryID   string `json:"entryId" validate:"omitempty,id"`
}

type SwitchBranchRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	BranchID  string `json:"branchId" validate:"id"`
	BindingID string `json:"bindingId" validate:"omitempty,id"`
	Name      string `json:"name" validate:"required,notblank,normalized,no_control"`
}
