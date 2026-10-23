package bindings

import (
	apperr "praxis/wails/error"
)

func publicError(host Host, operation string, err error) error {
	if err == nil {
		return nil
	}
	host.LogError("binding operation=%s failed: %v", operation, err)
	return apperr.PublicError(err)
}

func publicInputError(host Host, operation string, err error) error {
	if err == nil {
		return nil
	}
	host.LogError("binding operation=%s failed: %v", operation, err)
	return apperr.PublicInputError(err)
}
