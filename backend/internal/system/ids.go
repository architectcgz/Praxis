package system

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// SecureIDGenerator 生成不可预测的标识符。
type SecureIDGenerator struct{}

func (SecureIDGenerator) New(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate %s id: %v", prefix, err))
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}

// IDsOrDefault 在未注入生成器时返回生产环境使用的安全生成器。
func IDsOrDefault(ids IDGenerator) IDGenerator {
	if ids == nil {
		return SecureIDGenerator{}
	}
	return ids
}
