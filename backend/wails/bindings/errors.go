package bindings

import apperr "praxis/wails/error"

func publicError(runtime Runtime, operation string, err error) error {
	if err == nil {
		return nil
	}
	runtime.LogError("binding operation=%s failed: %v", operation, err)
	return apperr.PublicError(err)
}
