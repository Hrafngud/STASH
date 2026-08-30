package unit

import (
	"strings"
	"testing"
)

func TestParseRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Range
	}{
		{"0..100", Range{Min: 0, Max: 100}},
		{"80..2k", Range{Min: 80, Max: 2_000}},
		{".05..0.8", Range{Min: 0.05, Max: 0.8}},
		{"-1..1", Range{Min: -1, Max: 1}},
		{"0..100M", Range{Min: 0, Max: 100_000_000}},
		{"-2k..-1k", Range{Min: -2_000, Max: -1_000}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRange(test.input)
			if err != nil {
				t.Fatalf("ParseRange(%q) returned error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseRange(%q) = %#v, want %#v", test.input, got, test.want)
			}
		})
	}
}

func TestParseRangeRejectsMalformedOrNonIncreasingBounds(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"", "1", "..", "..1", "1..", "1...2", "1..2..3",
		"2..1", "1..1", "1e0..2", "0..2K", " 0..1", "0..1 ",
	}

	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseRange(input)
			if err == nil {
				t.Fatalf("ParseRange(%q) returned no error", input)
			}
			if !strings.Contains(err.Error(), "invalid range") {
				t.Fatalf("ParseRange(%q) error = %q, want useful context", input, err)
			}
		})
	}
}
