package primitive

import (
	"math"
	"strings"
	"testing"
)

func TestParseNoteAndCanonicalName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input      string
		wantNumber int64
		wantName   string
	}{
		{"C-1", 0, "C-1"},
		{"C4", 60, "C4"},
		{"C#4", 61, "C#4"},
		{"Db4", 61, "C#4"},
		{"A4", 69, "A4"},
		{"Bb5", 82, "A#5"},
		{"B#3", 60, "C4"},
		{"Cb4", 59, "B3"},
		{"Fb4", 64, "E4"},
		{"E#4", 65, "F4"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			note, err := ParseNote(test.input)
			if err != nil {
				t.Fatalf("ParseNote(%q) returned error: %v", test.input, err)
			}
			if note.Number() != test.wantNumber {
				t.Fatalf("ParseNote(%q).Number() = %d, want %d", test.input, note.Number(), test.wantNumber)
			}
			if note.String() != test.wantName {
				t.Fatalf("ParseNote(%q).String() = %q, want %q", test.input, note.String(), test.wantName)
			}
		})
	}
}

func TestNoteFrequency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  float64
	}{
		{"A3", 220},
		{"A4", 440},
		{"A5", 880},
		{"C4", 261.6255653005986},
	}
	for _, test := range tests {
		note, err := ParseNote(test.input)
		if err != nil {
			t.Fatalf("ParseNote(%q) returned error: %v", test.input, err)
		}
		if difference := math.Abs(note.Frequency() - test.want); difference > 1e-10 {
			t.Errorf("ParseNote(%q).Frequency() = %.15f, want %.15f", test.input, note.Frequency(), test.want)
		}
	}
}

func TestEnharmonicNotesHaveEqualFrequency(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{{"C#4", "Db4"}, {"D#5", "Eb5"}, {"F#2", "Gb2"}, {"G#7", "Ab7"}, {"A#0", "Bb0"}}
	for _, pair := range pairs {
		left, leftErr := ParseNote(pair[0])
		right, rightErr := ParseNote(pair[1])
		if leftErr != nil || rightErr != nil {
			t.Fatalf("ParseNote(%q, %q) errors = %v, %v", pair[0], pair[1], leftErr, rightErr)
		}
		if left.Number() != right.Number() || left.Frequency() != right.Frequency() {
			t.Errorf("%s and %s did not resolve enharmonically", pair[0], pair[1])
		}
	}
}

func TestParseNoteRejectsMalformedOrUnresolvableNotes(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"", "H4", "c4", "C", "4", "C##4", "Cbb4", "C4#", " C4", "C4 ", "C+4", "C4.0",
		"C9999999999999999999999999999999999", "C2000", "C-2000",
	}
	for _, input := range inputs {
		_, err := ParseNote(input)
		if err == nil {
			t.Errorf("ParseNote(%q) returned nil error", input)
			continue
		}
		if !strings.Contains(err.Error(), "invalid note") {
			t.Errorf("ParseNote(%q) error = %q, want invalid note context", input, err)
		}
	}
}

func TestParseNoteList(t *testing.T) {
	t.Parallel()

	notes, err := ParseNoteList("C4,E4,G4,C5")
	if err != nil {
		t.Fatalf("ParseNoteList returned error: %v", err)
	}
	if got := noteNames(notes); strings.Join(got, ",") != "C4,E4,G4,C5" {
		t.Fatalf("ParseNoteList names = %v", got)
	}

	for _, input := range []string{"", ",", "C4,", ",C4", "C4,,E4", "C4, E4", "C4:major"} {
		if _, err := ParseNoteList(input); err == nil {
			t.Errorf("ParseNoteList(%q) returned nil error", input)
		}
	}
}

func noteNames(notes []Note) []string {
	names := make([]string, len(notes))
	for index, note := range notes {
		names[index] = note.String()
	}
	return names
}
