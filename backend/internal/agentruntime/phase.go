package agentruntime

import "sync"

// AgentRuntimePhase is the process-local structural operation phase.
type AgentRuntimePhase string

const (
	PhaseIdle       AgentRuntimePhase = "idle"
	PhaseTurn       AgentRuntimePhase = "turn"
	PhaseCompaction AgentRuntimePhase = "compaction"
	PhaseRetry      AgentRuntimePhase = "retry"
	PhaseStopping   AgentRuntimePhase = "stopping"
)

// PhaseGuard serializes structural runtime operations.
type PhaseGuard struct {
	mu    sync.Mutex
	phase AgentRuntimePhase
}

type phaseGuard = PhaseGuard

func newPhaseGuard() *phaseGuard { return NewPhaseGuard() }

func (g *PhaseGuard) acquire(target AgentRuntimePhase) error { return g.Acquire(target) }

func (g *PhaseGuard) stop() error { return g.Stop() }

func (g *PhaseGuard) release() { g.Release() }

// NewPhaseGuard creates a guard in the idle phase.
func NewPhaseGuard() *PhaseGuard {
	return &PhaseGuard{phase: PhaseIdle}
}

// Current returns the current phase.
func (g *PhaseGuard) Current() AgentRuntimePhase {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.phase
}

// Acquire atomically checks that the guard is idle and takes target ownership.
func (g *PhaseGuard) Acquire(target AgentRuntimePhase) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.phase != PhaseIdle {
		return &RuntimeError{Code: ErrorBusy, Message: "runtime has an active structural operation"}
	}
	if target != PhaseTurn && target != PhaseCompaction && target != PhaseRetry {
		return &RuntimeError{Code: ErrorContract, Message: "invalid phase acquisition target"}
	}
	g.phase = target
	return nil
}

// Stop transitions an active operation to stopping without releasing its ownership.
func (g *PhaseGuard) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.phase != PhaseTurn && g.phase != PhaseCompaction && g.phase != PhaseRetry {
		return &RuntimeError{Code: ErrorBusy, Message: "runtime is not running"}
	}
	g.phase = PhaseStopping
	return nil
}

// Release returns the guard to idle after the owned operation has settled.
func (g *PhaseGuard) Release() {
	g.mu.Lock()
	g.phase = PhaseIdle
	g.mu.Unlock()
}
