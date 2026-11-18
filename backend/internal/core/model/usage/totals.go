package usage

// Totals 累计模型请求的已知计数；报告数用于区分未知分项与已知零值。
type Totals struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	CacheReadInputTokens     int64  `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64  `json:"cacheCreationInputTokens"`
	ReportedRequests         uint64 `json:"reportedRequests"`
	InputReports             uint64 `json:"inputReports"`
	OutputReports            uint64 `json:"outputReports"`
	CacheReadReports         uint64 `json:"cacheReadReports"`
	CacheCreationReports     uint64 `json:"cacheCreationReports"`
}

func (u *Totals) Add(value Usage) {
	if !value.Reported() {
		return
	}
	u.ReportedRequests++
	if value.InputTokens != nil {
		u.InputTokens += *value.InputTokens
		u.InputReports++
	}
	if value.OutputTokens != nil {
		u.OutputTokens += *value.OutputTokens
		u.OutputReports++
	}
	if value.CacheReadInputTokens != nil {
		u.CacheReadInputTokens += *value.CacheReadInputTokens
		u.CacheReadReports++
	}
	if value.CacheCreationInputTokens != nil {
		u.CacheCreationInputTokens += *value.CacheCreationInputTokens
		u.CacheCreationReports++
	}
}
