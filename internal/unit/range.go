package unit

import (
	"fmt"
	"strings"
)

// Range is a strictly increasing pair of numeric bounds.
type Range struct {
	Min float64
	Max float64
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
