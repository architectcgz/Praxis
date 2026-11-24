package streaming

import "encoding/json"

// DecodeUsage 独立解码可选统计到协议 DTO；缺失或格式无效时返回 false，不影响正文。
// 诊断只记录协议名称，不记录响应载荷或可能包含敏感内容的解析错误。
func DecodeUsage(data *json.RawMessage, target any, protocol string, warnf func(string, ...any)) bool {
	if data == nil {
		return false
	}
	if err := json.Unmarshal(*data, target); err != nil {
		if warnf != nil {
			warnf("ignoring invalid model usage protocol=%s reason=invalid_schema", protocol)
		}
		return false
	}
	return true
}
