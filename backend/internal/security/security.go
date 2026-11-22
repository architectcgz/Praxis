package security

// ApprovalMode 是单次操作的处理动作。
type ApprovalMode string

const (
	ApprovalAsk  ApprovalMode = "ask"
	ApprovalAuto ApprovalMode = "auto"
	ApprovalYolo ApprovalMode = "yolo"
)

// Valid 校验全局或会话审批模式。
func (m ApprovalMode) Valid() bool {
	return m == ApprovalAsk || m == ApprovalAuto || m == ApprovalYolo
}

// EffectiveApprovalMode 优先采用会话覆盖，nil 表示继承全局默认动作。
func EffectiveApprovalMode(global ApprovalMode, session *ApprovalMode) ApprovalMode {
	if session != nil {
		return *session
	}
	return global
}

// ApprovalDecision 保存本次操作生效的动作及来源，不构成对未来调用的授权。
type ApprovalDecision struct {
	Action ApprovalMode
	RuleID string
}

// ResolveApprovalDecision 将有效审批模式转换为本次操作的决定。
func ResolveApprovalDecision(mode ApprovalMode) ApprovalDecision {
	if mode == ApprovalYolo {
		return ApprovalDecision{Action: ApprovalYolo, RuleID: "yolo"}
	}
	return ApprovalDecision{Action: mode, RuleID: "default"}
}

// RiskVerdict 是模型审核结果，不与操作处理动作混用。
type RiskVerdict string

const (
	RiskSafe      RiskVerdict = "safe"
	RiskDangerous RiskVerdict = "dangerous"
	RiskUncertain RiskVerdict = "uncertain"
)

// RiskAssessment 只保存输入边界已校验的可展示结论，不保存模型思维过程。
type RiskAssessment struct {
	Verdict RiskVerdict `json:"verdict"`
	Reason  string      `json:"reason"`
}
