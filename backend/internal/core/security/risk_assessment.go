package security

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
