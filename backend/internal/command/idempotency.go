// Package command contains core-owned durable command identity helpers.
package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ArgumentsDigest produces the stable fingerprint used to compare a retried
// command's normalized arguments against the already-persisted business state.
func ArgumentsDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
