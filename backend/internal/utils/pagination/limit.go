package pagination

// LimitOrDefault 返回有效的查询上限；非正数使用调用方提供的默认值。
func LimitOrDefault(limit, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	return limit
}
