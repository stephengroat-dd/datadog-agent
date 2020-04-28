// +build linux_bpf

package netlink

import (
	"sync/atomic"
	"time"
)

const (
	tickInterval  = 1 * time.Second
	breakerOpen   = int64(1)
	breakerClosed = int64(1)

	// The lower this number is the more amortized the average is
	// For example, if ewmaWeight is 1, a single burst of events might
	// cause the breaker to trip.
	ewmaWeight = 0.2
)

// CircuitBreaker is meant to enforce a maximum rate of events per second
// Once the event rate goes above the threshold the circuit breaker will trip
// and remain open until Reset() is called.
type CircuitBreaker struct {
	// The maximum rate of events allowed to pass
	maxEventsPerSec int

	// The number of events elapsed since the last tick
	eventCount int64

	// An exponentially weighted average of the event rate (per second)
	// This is what actually compare against maxEventsPersec
	eventRate int64

	// Represents the status of the cicuit breaker.
	// 1 means open, 0 means closed
	status int64

	// The timestamp in nanoseconds of when we last updated eventRate
	lastUpdate int64
}

func NewCircuitBreaker(maxEventsPerSec int) *CircuitBreaker {
	c := &CircuitBreaker{maxEventsPerSec: maxEventsPerSec}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for t := range ticker.C {
			c.update(t)
		}
	}()

	return c
}

func (c *CircuitBreaker) IsOpen() bool {
	return atomic.LoadInt64(&c.status) == breakerOpen
}

func (c *CircuitBreaker) Tick(n int) {
	atomic.AddInt64(&c.eventCount, int64(n))
}

func (c *CircuitBreaker) Reset() {
	atomic.StoreInt64(&c.eventCount, 0)
	atomic.StoreInt64(&c.status, breakerClosed)
	atomic.StoreInt64(&c.eventRate, 0)
	atomic.StoreInt64(&c.lastUpdate, time.Now().UnixNano())
}

func (c *CircuitBreaker) update(now time.Time) {
	if c.IsOpen() {
		return
	}

	lastUpdate := atomic.LoadInt64(&c.lastUpdate)
	deltaInSec := float64(now.UnixNano()-lastUpdate) / float64(time.Second.Nanoseconds())
	if deltaInSec < 1.0 {
		// This is to avoid a divide by 0 panic or a spike due
		// to a reset followed immeditialy by an update call
		return
	}

	// Calculate the event rate (EWMA)
	eventCount := atomic.SwapInt64(&c.eventCount, 0)
	prevEventRate := atomic.LoadInt64(&c.eventRate)
	newEventRate := ewmaWeight*float64(eventCount)/deltaInSec + (1-ewmaWeight)*float64(prevEventRate)

	// Update circuit breaker status accordingly
	if int(newEventRate) > c.maxEventsPerSec {
		atomic.StoreInt64(&c.status, breakerOpen)
	}

	atomic.StoreInt64(&c.lastUpdate, now.UnixNano())
	atomic.StoreInt64(&c.eventRate, int64(newEventRate))
}
