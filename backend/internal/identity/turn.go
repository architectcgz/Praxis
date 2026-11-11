package identity

import (
	"fmt"
)

// NewTurnID 按 Run 内实际模型请求序号生成，包含普通请求、审核和摘要。
func NewTurnID(runID RunID, sequence uint64) TurnID {
	return TurnID(fmt.Sprintf("turn:%s:%d", runID, sequence))
}

func NewModelRequestID() ModelRequestID {
	return ModelRequestID(SecureIDGenerator{}.New("request"))
}
