package turn

// ValidTurnOutcome 判断回合结果是否受支持。
func ValidTurnOutcome(outcome TurnOutcome) bool { return validTurnOutcome(outcome) }
