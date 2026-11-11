package streaming

import (
	"encoding/json"
	"praxis/internal/logging"
)

var usageLogger = logging.NewFactory().Console()

// DecodeUsage 独立解码可选统计到协议 DTO；缺失或格式无效时返回 false，不影响正文。
// 诊断只记录协议名称，不记录响应载荷或可能包含敏感内容的解析错误。
func DecodeUsage(data *json.RawMessage, target any, protocol string) bool {
	if data == nil {
		return false
	}
	if err := json.Unmarshal(*data, target); err != nil {
		usageLogger.Warnf("ignoring invalid model usage protocol=%s reason=invalid_schema", protocol)
		return false
	}
	return true
}
