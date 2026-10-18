package tool

// ToolExecutionResult 区分已结算结果与待审批调用。
type ToolExecutionResult struct {
	Result          ToolResult
	ApprovalPending bool
}
