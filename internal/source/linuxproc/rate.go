package linuxproc

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var ErrCounterReset = errors.New("counter reset or wrapped")

func counterRate(previous, current uint64, previousAt, currentAt time.Time, multiplier uint64) (float64, error) {
	if current < previous {
		return 0, fmt.Errorf("%w: moved from %d to %d", ErrCounterReset, previous, current)
	}
	elapsed := currentAt.Sub(previousAt)
	if elapsed <= 0 {
		return 0, fmt.Errorf("rate interval must be positive")
	}
	delta := current - previous
	if multiplier != 0 && delta > math.MaxUint64/multiplier {
		return 0, fmt.Errorf("rate delta overflow")
	}
	return float64(delta*multiplier) / elapsed.Seconds(), nil
}

func isCounterReset(err error) bool { return errors.Is(err, ErrCounterReset) }
