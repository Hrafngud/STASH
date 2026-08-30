// Package unit parses the shell-safe numeric, duration, and range grammars
// exposed by the STASH command line.
package unit

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

var numberPattern = regexp.MustCompile(`^-?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)([kMG])?$`)

// ParseNumber parses a decimal number with an optional case-sensitive decimal
// magnitude suffix: k, M, or G.
func ParseNumber(input string) (float64, error) {
	match := numberPattern.FindStringSubmatch(input)
	if match == nil {
		return 0, fmt.Errorf("invalid number %q: expected a decimal with optional k, M, or G suffix", input)
	}

	numeric := input
	multiplier := 1.0
	if suffix := match[1]; suffix != "" {
		numeric = input[:len(input)-1]
		switch suffix {
		case "k":
			multiplier = 1_000
		case "M":
			multiplier = 1_000_000
		case "G":
			multiplier = 1_000_000_000
		}
	}

	value, err := strconv.ParseFloat(numeric, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", input, err)
	}
	value *= multiplier
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, fmt.Errorf("invalid number %q: value is not finite", input)
	}
	return value, nil
}
