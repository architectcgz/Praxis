package system

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// SecureIDGenerator creates unpredictable opaque identifiers.
type SecureIDGenerator struct{}

func (SecureIDGenerator) New(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate %s id: %v", prefix, err))
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
