package app

import "praxis/internal/contracts"

type SystemBindings struct {
	runtime *bindingRuntime
}

// Ping is the desktop binding smoke endpoint.
func (b *SystemBindings) Ping() string {
	b.runtime.logDebug("binding Ping")
	return "pong"
}

func (b *SystemBindings) Readiness() contracts.HealthSnapshot {
	return b.runtime.health()
}
