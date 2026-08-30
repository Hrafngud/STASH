package unit

import (
	"math"
	"strings"
	"testing"
)

func TestParseNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  float64
	}{
		{"0", 0},
		{"1", 1},
		{".1", 0.1},
		{"0.1", 0.1},
		{"12.5", 12.5},
		{"1.", 1},
		{"-1", -1},
		{"-.5", -0.5},
		{"2k", 2_000},
		{"8k", 8_000},
		{"100M", 100_000_000},
		{"1G", 1_000_000_000},
		{"-1.5k", -1_500},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseNumber(test.input)
			if err != nil {
				t.Fatalf("ParseNumber(%q) returned error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseNumber(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestParseNumberRejectsMalformedAndNonFiniteValues(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"", ".", "-", "+1", "1e3", "1_000", " 1", "1 ",
		"1K", "1m", "1g", "NaN", "Inf", "-Inf", "1kk", "1.2.3",
		"999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999G",
	}

	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			value, err := ParseNumber(input)
			if err == nil {
				t.Fatalf("ParseNumber(%q) = %v, want error", input, value)
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("ParseNumber(%q) returned non-finite value %v", input, value)
			}
			if !strings.Contains(err.Error(), "invalid number") {
				t.Fatalf("ParseNumber(%q) error = %q, want useful context", input, err)
			}
		})
	}
}
