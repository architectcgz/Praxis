package run

import providerapi "praxis/internal/core/provider"

// UsageTotals 累计已上报请求的已知计数；报告数用于区分未知分项与已知零值。
type UsageTotals struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	CacheReadInputTokens     int64  `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64  `json:"cacheCreationInputTokens"`
	ReportedRequests         uint64 `json:"reportedRequests"`
	OutputReports            uint64 `json:"outputReports"`
	CacheReadReports         uint64 `json:"cacheReadReports"`
	CacheCreationReports     uint64 `json:"cacheCreationReports"`
}

func (u *UsageTotals) Add(value providerapi.Usage) {
	u.ReportedRequests++
	u.InputTokens += value.InputTokens
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

func (u *UsageTotals) Merge(value UsageTotals) {
	u.InputTokens += value.InputTokens
	u.OutputTokens += value.OutputTokens
	u.CacheReadInputTokens += value.CacheReadInputTokens
	u.CacheCreationInputTokens += value.CacheCreationInputTokens
	u.ReportedRequests += value.ReportedRequests
	u.OutputReports += value.OutputReports
	u.CacheReadReports += value.CacheReadReports
	u.CacheCreationReports += value.CacheCreationReports
}
