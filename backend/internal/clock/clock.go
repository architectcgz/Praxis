package clock

import (
	"time"
)

// Clock 提供可注入的执行时间来源。
type Clock interface {
	Now() time.Time
}

// ClockOrDefault 在未注入时钟时返回生产环境使用的 UTC 时钟。
func ClockOrDefault(clock Clock) Clock {
	if clock == nil {
		return UTCClock{}
	}
	return clock
}

// UTCClock 是生产环境时钟，供存储适配器在编排事务外为追加记录标记时间。
type UTCClock struct{}

func (UTCClock) Now() time.Time { return time.Now().UTC() }
