package run

import (
	"praxis/internal/core/loop"
)

func storageFailure(err error) error {
	return loop.NewFailure(loop.FailureStorage, err)
}
