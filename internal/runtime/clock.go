package runtime

import "time"

// Clock is the small scheduling surface used by the control engine. Tests can
// inject a manually advanced clock without changing source sample timestamps.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time                             { return time.Now() }
func (wallClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }
