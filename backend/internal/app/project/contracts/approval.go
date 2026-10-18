package contracts

// ProjectApprovalMode 返回项目选择及后端解析的有效授权模式。
type ProjectApprovalMode struct {
	ProjectID     string
	GlobalMode    string
	ProjectMode   *string
	EffectiveMode string
}
