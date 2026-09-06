package system

import "time"

// UTCClock is the production clock used by storage adapters that stamp
// append-only records outside an orchestration transaction.
type UTCClock struct{}

func (UTCClock) Now() time.Time { return time.Now().UTC() }
