package runtimetest

import (
	"sync"
	"time"
)

// FakeClock provides a deterministic clock for lifecycle assertions.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock creates a fake clock at now.
func NewFakeClock(now time.Time) *FakeClock { return &FakeClock{now: now.UTC()} }

// Now implements agentruntime.Clock.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake clock forward.
func (c *FakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

// Set replaces the current fake time.
func (c *FakeClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now.UTC()
	c.mu.Unlock()
}

// FakeIDGenerator generates deterministic identifiers for test metadata.
type FakeIDGenerator struct {
	mu      sync.Mutex
	counter uint64
}

// New returns a deterministic identifier with the supplied prefix.
func (g *FakeIDGenerator) New(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counter++
	return prefix + "-" + formatUint(g.counter)
}

func formatUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
