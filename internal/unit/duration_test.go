package unit

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  time.Duration
	}{
		{"0ms", 0},
		{"5ms", 5 * time.Millisecond},
		{"150ms", 150 * time.Millisecond},
		{".5ms", 500 * time.Microsecond},
		{"1s", time.Second},
		{"1.5s", 1500 * time.Millisecond},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDuration(test.input)
			if err != nil {
				t.Fatalf("ParseDuration(%q) returned error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseDuration(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestParseDurationRejectsInvalidGrammar(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"", "1", "-1s", "+1s", "1m", "1us", "1ns", "1MS", "1S",
		"1e3ms", "NaNs", "Infms", " 1s", "1s ", "1.2.3s",
		"999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999s",
	}

	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseDuration(input)
			if err == nil {
				t.Fatalf("ParseDuration(%q) returned no error", input)
			}
			if !strings.Contains(err.Error(), "invalid duration") {
				t.Fatalf("ParseDuration(%q) error = %q, want useful context", input, err)
			}
		})
	}
}
