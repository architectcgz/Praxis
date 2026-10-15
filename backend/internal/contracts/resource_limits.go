package contracts

type ResourceLimits struct {
	MaxToolCalls   int
	MaxInputBytes  int64
	MaxOutputBytes int64
}

func (r ResourceLimits) Validate() error {
	if r.MaxToolCalls < 0 || r.MaxInputBytes < 0 || r.MaxOutputBytes < 0 {
		return InvalidValue("task.resourceLimits", "resource limits cannot be negative")
	}
	return nil
}
