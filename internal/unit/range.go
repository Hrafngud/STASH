package unit

import (
	"fmt"
	"strings"
	"time"
)

// Range is a strictly increasing pair of numeric bounds.
type Range struct {
	Min float64
	Max float64
}

// ParseTypedRange extends ParseRange with duration pairs. Duration values are
// represented as seconds so the control and audio layers remain numeric.
func ParseTypedRange(input string) (Range, string, error) {
	if parsed, err := ParseRange(input); err == nil {
		return parsed, "", nil
	}
	if strings.Count(input, "..") != 1 {
		return Range{}, "", fmt.Errorf("invalid range %q: expected MIN..MAX", input)
	}
	parts := strings.SplitN(input, "..", 2)
	minimum, err := ParseDuration(parts[0])
	if err != nil {
		return Range{}, "", fmt.Errorf("invalid range %q: MIN: %w", input, err)
	}
	maximum, err := ParseDuration(parts[1])
	if err != nil {
		return Range{}, "", fmt.Errorf("invalid range %q: MAX: %w", input, err)
	}
	if minimum >= maximum {
		return Range{}, "", fmt.Errorf("invalid range %q: MIN must be less than MAX", input)
	}
	return Range{Min: float64(minimum) / float64(time.Second), Max: float64(maximum) / float64(time.Second)}, "time", nil
}

// ParseRange parses MIN..MAX using the public STASH numeric grammar.
func ParseRange(input string) (Range, error) {
	if strings.Count(input, "..") != 1 {
		return Range{}, fmt.Errorf("invalid range %q: expected MIN..MAX", input)
	}
	parts := strings.SplitN(input, "..", 2)
	if parts[0] == "" || parts[1] == "" {
		return Range{}, fmt.Errorf("invalid range %q: expected both MIN and MAX", input)
	}

	min, err := ParseNumber(parts[0])
	if err != nil {
		return Range{}, fmt.Errorf("invalid range %q: MIN: %w", input, err)
	}
	max, err := ParseNumber(parts[1])
	if err != nil {
		return Range{}, fmt.Errorf("invalid range %q: MAX: %w", input, err)
	}
	if min >= max {
		return Range{}, fmt.Errorf("invalid range %q: MIN must be less than MAX", input)
	}

	return Range{Min: min, Max: max}, nil
}
