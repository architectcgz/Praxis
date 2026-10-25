package identity

// Valid 判断项目身份是否满足统一身份契约，不执行规范化。
func (id ProjectID) Valid() bool { return validResourceID(string(id)) }

// Valid 判断会话身份是否满足统一身份契约，不执行规范化。
func (id SessionID) Valid() bool { return validResourceID(string(id)) }

// Project、Session 身份由外部生成，须能在所有支持的平台上直接作为资源名称使用。
// 统一限制字符、长度和保留名称，避免入口通过后在文件操作阶段才发现身份不可用。
func validResourceID(value string) bool {
	if len(value) == 0 || len(value) > 180 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	switch value {
	case "con", "prn", "aux", "nul":
		return false
	}
	if len(value) == 4 && (value[:3] == "com" || value[:3] == "lpt") && value[3] >= '1' && value[3] <= '9' {
		return false
	}
	return true
}
