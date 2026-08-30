package control

import (
	"fmt"
	"math"
	"time"
)

// Smoother is a deterministic first-order low-pass smoother. Its time
// constant is supplied at construction and progression occurs only through
// explicit delta durations passed to Step.
type Smoother struct {
	timeConstant time.Duration
	value        float64
}

// NewSmoother constructs a smoother with an explicit initial output value.
func NewSmoother(timeConstant time.Duration, initial float64) (*Smoother, error) {
	if timeConstant < 0 {
		return nil, fmt.Errorf("invalid smoothing duration %s: must be non-negative", timeConstant)
	}
	if err := validateFinite("initial smoothing value", initial); err != nil {
		return nil, err
	}
	return &Smoother{timeConstant: timeConstant, value: initial}, nil
}

// Value returns the current smoothed output.
func (smoother *Smoother) Value() float64 {
	return smoother.value
}

// Reset sets the output directly without advancing time.
func (smoother *Smoother) Reset(value float64) error {
	if err := validateFinite("smoothing reset value", value); err != nil {
		return err
	}
	smoother.value = value
	return nil
}

// Step advances toward target by delta. A zero time constant bypasses
// smoothing. For a positive time constant, alpha is 1-exp(-delta/tau).
func (smoother *Smoother) Step(target float64, delta time.Duration) (float64, error) {
	if err := validateFinite("smoothing target", target); err != nil {
		return 0, err
	}
	if delta < 0 {
		return 0, fmt.Errorf("invalid smoothing delta %s: must be non-negative", delta)
	}
	if smoother.timeConstant == 0 {
		smoother.value = target
		return smoother.value, nil
	}
	if delta == 0 {
		return smoother.value, nil
	}

	ratio := float64(delta) / float64(smoother.timeConstant)
	alpha := -math.Expm1(-ratio)
	smoother.value = (1-alpha)*smoother.value + alpha*target
	return smoother.value, nil
}
