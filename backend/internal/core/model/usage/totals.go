package usage

// Totals 累计模型请求的已知计数；报告数用于区分未知分项与已知零值。
type Totals struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	CacheReadInputTokens     int64  `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64  `json:"cacheCreationInputTokens"`
	ReportedRequests         uint64 `json:"reportedRequests"`
	OutputReports            uint64 `json:"outputReports"`
	CacheReadReports         uint64 `json:"cacheReadReports"`
	CacheCreationReports     uint64 `json:"cacheCreationReports"`
}

func (u *Totals) Add(input int64, output, cacheRead, cacheCreation *int64) {
	u.ReportedRequests++
	u.InputTokens += input
	if output != nil {
		u.OutputTokens += *output
		u.OutputReports++
	}
	if cacheRead != nil {
		u.CacheReadInputTokens += *cacheRead
		u.CacheReadReports++
	}
	if cacheCreation != nil {
		u.CacheCreationInputTokens += *cacheCreation
		u.CacheCreationReports++
	}
}

// Subtract 用于以同一 Run 的新投影替换旧值，或移除已删除分支所属 Run。
func (u *Totals) Subtract(value Totals) {
	u.InputTokens -= value.InputTokens
	u.OutputTokens -= value.OutputTokens
	u.CacheReadInputTokens -= value.CacheReadInputTokens
	u.CacheCreationInputTokens -= value.CacheCreationInputTokens
	u.ReportedRequests -= value.ReportedRequests
	u.OutputReports -= value.OutputReports
	u.CacheReadReports -= value.CacheReadReports
	u.CacheCreationReports -= value.CacheCreationReports
}

func (u *Totals) Merge(value Totals) {
	u.InputTokens += value.InputTokens
	u.OutputTokens += value.OutputTokens
	u.CacheReadInputTokens += value.CacheReadInputTokens
	u.CacheCreationInputTokens += value.CacheCreationInputTokens
	u.ReportedRequests += value.ReportedRequests
	u.OutputReports += value.OutputReports
	u.CacheReadReports += value.CacheReadReports
	u.CacheCreationReports += value.CacheCreationReports
}
