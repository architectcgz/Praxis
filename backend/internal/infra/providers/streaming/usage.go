package streaming

import (
	"encoding/json"

	"praxis/internal/core/model"
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
		usageLogger.Warnf("忽略无效模型用量 protocol=%s reason=invalid_schema", protocol)
		return false
	}
	return true
}

// ValidUsage 校验转换后的实际统计字段；无效或不完整计数只记诊断，不应更新统计。
func ValidUsage(protocol string, usage *model.ModelUsage) bool {
	if usage == nil || !usage.Valid() {
		usageLogger.Warnf("忽略无效模型用量 protocol=%s reason=invalid_counts", protocol)
		return false
	}
	return true
}
