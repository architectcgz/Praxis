package orchestrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	domaincommand "praxis/internal/core/domain/command"
	domainfoundation "praxis/internal/core/domain/foundation"

	"praxis/internal/core/persistence"
)

func commandArgumentsDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func commandReceipt(
	ctx context.Context,
	repository persistence.CommandReceiptRepository,
	requestID domainfoundation.RequestID,
	command string,
	digest string,
) (domaincommand.CommandReceipt, bool, error) {
	if repository == nil || requestID == "" {
		return domaincommand.CommandReceipt{}, false, errors.New("command receipt repository and request id are required")
	}
	receipt, err := repository.Get(ctx, requestID)
	if errors.Is(err, domainfoundation.ErrNotFound) {
		return domaincommand.CommandReceipt{}, false, nil
	}
	if err != nil {
		return domaincommand.CommandReceipt{}, false, err
	}
	if receipt.Command != command || receipt.ArgumentsDigest != digest {
		return domaincommand.CommandReceipt{}, false, domainfoundation.ErrRequestConflict
	}
	return receipt, true, nil
}
