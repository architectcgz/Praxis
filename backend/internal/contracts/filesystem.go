package contracts

import "errors"

var (
	ErrFileTooLarge   = errors.New("file exceeds the size limit")
	ErrNotRegularFile = errors.New("file is not a regular file")
	ErrInvalidText    = errors.New("file is not valid UTF-8 text")
)
