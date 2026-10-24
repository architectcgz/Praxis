package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

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
