package execution

import (
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/loop"
)

func storageFailure(err error) error {
	return loop.NewFailure(loop.FailureStorage, err)
}

func modelFailure(err error) error {
	if _, ok := errors.AsType[*loop.Failure](err); ok {
		return err
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return loop.NewFailure(loop.FailureResourceLimit, fmt.Errorf("model context window exceeded: %w", err))
	}
	return loop.NewFailure(loop.FailureProvider, err)
}
