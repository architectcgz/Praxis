package entry

import "strings"

// Preview 提供历史目录的短文本，不保存第二份正文。
func (e ContextEntry) Preview() string {
	if e.IsRun() {
		return "执行结束 · " + e.ExecutionResult().Outcome
	}
	if e.IsSummary() {
		return "上下文摘要"
	}
	text := strings.Join(strings.Fields(e.TextContent()), " ")
	if text == "" {
		text = string(e.Role)
	}
	runes := []rune(text)
	if len(runes) > 100 {
		text = string(runes[:100]) + "…"
	}
	return text
}
