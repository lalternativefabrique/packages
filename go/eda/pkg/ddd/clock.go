package ddd

import "time"

// Clock abstracts time.Now for testability. Implementations must be
// goroutine-safe.
type Clock interface {
	Now() time.Time
}

// SystemClock returns time.Now().
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// FixedClock always returns the same instant. Useful in tests.
type FixedClock struct {
	T time.Time
}

func (c FixedClock) Now() time.Time { return c.T }
