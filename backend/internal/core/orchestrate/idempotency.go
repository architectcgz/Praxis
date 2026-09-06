package orchestrate

import (
	"context"
	domaincommand "praxis/internal/core/domain/command"
	domainfoundation "praxis/internal/core/domain/foundation"

	corecommand "praxis/internal/core/command"
	"praxis/internal/core/persistence"
)

func commandArgumentsDigest(value any) string {
	return corecommand.ArgumentsDigest(value)
}

func commandReceipt(
	ctx context.Context,
	repository persistence.CommandReceiptRepository,
	requestID domainfoundation.RequestID,
	command string,
	digest string,
) (domaincommand.CommandReceipt, bool, error) {
	return corecommand.FindReceipt(ctx, repository, requestID, command, digest)
}
