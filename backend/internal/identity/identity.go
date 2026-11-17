// Package identity 定义内核使用的强类型标识，不承载应用层公开契约。
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// ID 使用不同类型，防止仓储意外交换不同聚合的标识符，
// 同时允许所有持久化格式继续使用字符串。
type (
	ProjectID         string
	SessionID         string
	AgentDefinitionID string
	AgentID           string
	RunID             string
	TaskID            string
	TurnID            string
	RequestID         string
	ModelRequestID    string
)

func (id ProjectID) String() string         { return string(id) }
func (id SessionID) String() string         { return string(id) }
func (id AgentDefinitionID) String() string { return string(id) }
func (id AgentID) String() string           { return string(id) }
func (id RunID) String() string             { return string(id) }
func (id TurnID) String() string            { return string(id) }
func (id RequestID) String() string         { return string(id) }
func (id ModelRequestID) String() string    { return string(id) }
func (id TaskID) String() string            { return string(id) }

// IDGenerator 为持久化对象分配身份。
type IDGenerator interface {
	New(prefix string) string
}

// SecureIDGenerator 生成不可预测的标识符。
type SecureIDGenerator struct{}

func (SecureIDGenerator) New(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate %s id: %v", prefix, err))
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}

// NewTurnID 按 Run 内实际模型请求序号生成，包含普通请求、审核和摘要。
func NewTurnID(runID RunID, sequence uint64) TurnID {
	return TurnID(fmt.Sprintf("turn:%s:%d", runID, sequence))
}

func NewModelRequestID() ModelRequestID {
	return ModelRequestID(SecureIDGenerator{}.New("request"))
}
