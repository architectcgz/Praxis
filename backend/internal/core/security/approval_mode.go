package security

// ApprovalMode 是单次操作的处理动作；deny 是硬性禁止，不能被 yolo 覆盖。
type ApprovalMode string

const (
	ApprovalAsk  ApprovalMode = "ask"
	ApprovalAuto ApprovalMode = "auto"
	ApprovalYolo ApprovalMode = "yolo"
	ApprovalDeny ApprovalMode = "deny"
)

// Valid 校验审批动作，包括工具禁用时的 deny。
func (m ApprovalMode) Valid() bool {
	return m == ApprovalAsk || m == ApprovalAuto || m == ApprovalYolo || m == ApprovalDeny
}

// ValidDefault 校验全局或会话默认动作；deny 用于工具禁用决定。
func (m ApprovalMode) ValidDefault() bool { return m.Valid() && m != ApprovalDeny }

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
