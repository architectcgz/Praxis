// Package command contains core-owned durable command identity helpers.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	domaincommand "praxis/internal/domain/command"
	foundation "praxis/internal/domain/foundation"
	"praxis/internal/persistence"
)

// ArgumentsDigest produces the stable fingerprint used to reject a retried
// command whose request identity is paired with different arguments.
func ArgumentsDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// FindReceipt loads a prior command result and verifies that it belongs to
// the same command name and argument fingerprint.
func FindReceipt(
	ctx context.Context,
	repository persistence.CommandReceiptRepository,
	requestID foundation.RequestID,
	name string,
	digest string,
) (domaincommand.CommandReceipt, bool, error) {
	if repository == nil || requestID == "" {
		return domaincommand.CommandReceipt{}, false, errors.New("command receipt repository and request id are required")
	}
	receipt, err := repository.Get(ctx, requestID)
	if errors.Is(err, foundation.ErrNotFound) {
		return domaincommand.CommandReceipt{}, false, nil
	}
	if err != nil {
		return domaincommand.CommandReceipt{}, false, err
	}
	if receipt.Command != name || receipt.ArgumentsDigest != digest {
		return domaincommand.CommandReceipt{}, false, foundation.ErrRequestConflict
	}
	return receipt, true, nil
}
