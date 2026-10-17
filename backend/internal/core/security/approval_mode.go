package security

// ApprovalMode 是单次操作的处理动作；deny 是硬性禁止，不能被 yolo 覆盖。
type ApprovalMode string

const (
	ApprovalAsk  ApprovalMode = "ask"
	ApprovalAuto ApprovalMode = "auto"
	ApprovalYolo ApprovalMode = "yolo"
	ApprovalDeny ApprovalMode = "deny"
)

// Valid 校验规则动作，不接受旧的 allow。
func (m ApprovalMode) Valid() bool {
	return m == ApprovalAsk || m == ApprovalAuto || m == ApprovalYolo || m == ApprovalDeny
}

// ValidDefault 校验全局或项目默认动作；deny 只用于明确禁止的规则。
func (m ApprovalMode) ValidDefault() bool { return m.Valid() && m != ApprovalDeny }

// EffectiveApprovalMode 优先采用项目覆盖，nil 表示继承全局默认动作。
func EffectiveApprovalMode(global ApprovalMode, project *ApprovalMode) ApprovalMode {
	if project != nil {
		return *project
	}
	return global
}

// ApprovalDecision 保存本次操作生效的动作及来源，不构成对未来调用的授权。
type ApprovalDecision struct {
	Action ApprovalMode
	RuleID string
}

// ResolveApprovalDecision 按硬性 deny、统一 yolo、具体规则、默认动作的顺序决策。
func ResolveApprovalDecision(mode ApprovalMode, matched ApprovalDecision) ApprovalDecision {
	if matched.Action == ApprovalDeny {
		return matched
	}
	if mode == ApprovalYolo {
		return ApprovalDecision{Action: ApprovalYolo, RuleID: "yolo"}
	}
	if matched.Action != "" {
		return matched
	}
	return ApprovalDecision{Action: mode, RuleID: "default"}
}
