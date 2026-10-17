package security

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// RiskVerdict 是模型审核结果，不与操作处理动作混用。
type RiskVerdict string

const (
	RiskSafe      RiskVerdict = "safe"
	RiskDangerous RiskVerdict = "dangerous"
	RiskUncertain RiskVerdict = "uncertain"
)

// RiskAssessment 只保存可展示的简短结论，不保存模型思维过程。
type RiskAssessment struct {
	Verdict RiskVerdict `json:"verdict"`
	Reason  string      `json:"reason"`
}

// Validate 拒绝未知结论及空、超长或非 canonical 的原因，不能将异常当作安全。
func (a RiskAssessment) Validate() error {
	if a.Verdict != RiskSafe && a.Verdict != RiskDangerous && a.Verdict != RiskUncertain {
		return errors.New("risk assessment has an invalid verdict")
	}
	if a.Reason == "" || a.Reason != strings.TrimSpace(a.Reason) || len(a.Reason) > 1000 || !utf8.ValidString(a.Reason) {
		return errors.New("risk assessment requires a canonical reason within 1000 bytes")
	}
	return nil
}
