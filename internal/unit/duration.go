package unit

import (
	"fmt"
	"regexp"
	"time"
)

var durationPattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:ms|s)$`)

// ParseDuration parses a non-negative duration whose unit is explicitly ms or
// s. Bare values and all other Go duration units are intentionally rejected.
func ParseDuration(input string) (time.Duration, error) {
	if !durationPattern.MatchString(input) {
		return 0, fmt.Errorf("invalid duration %q: expected a non-negative value ending in ms or s", input)
	}

	duration, err := time.ParseDuration(input)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", input, err)
	}
	return duration, nil
}
