package system

import "time"

// ClockOrDefault returns the production UTC clock when no clock is injected.
func ClockOrDefault(clock Clock) Clock {
	if clock == nil {
		return UTCClock{}
	}
	return clock
}

// UTCClock is the production clock used by storage adapters that stamp
// append-only records outside an orchestration transaction.
type UTCClock struct{}

func (UTCClock) Now() time.Time { return time.Now().UTC() }
