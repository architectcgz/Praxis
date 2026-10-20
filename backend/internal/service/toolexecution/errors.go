package toolexecution

import "errors"

var (
	errAuthorizationChanged = errors.New("tool authorization changed before start")
	errStopRequested        = errors.New("tool stop was requested before start")
)
