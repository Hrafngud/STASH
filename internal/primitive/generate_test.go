package primitive

import (
	"reflect"
	"strings"
	"testing"
)

func TestGenerateEveryScaleAtExactLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want []string
	}{
		{"major", []string{"C4", "D4", "E4", "F4", "G4", "A4", "B4", "C5"}},
		{"minor", []string{"C4", "D4", "D#4", "F4", "G4", "G#4", "A#4", "C5"}},
		{"chromatic", []string{"C4", "C#4", "D4", "D#4", "E4", "F4", "F#4", "G4", "G#4", "A4", "A#4", "B4", "C5"}},
		{"pentatonic-major", []string{"C4", "D4", "E4", "G4", "A4", "C5", "D5", "E5"}},
		{"pentatonic-minor", []string{"C4", "D#4", "F4", "G4", "A#4", "C5", "D#5", "F5"}},
	}
	root, err := ParseNote("C4")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			notes, err := GenerateScale(root, test.name, len(test.want))
			if err != nil {
				t.Fatalf("GenerateScale returned error: %v", err)
			}
			if got := noteNames(notes); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("GenerateScale names = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGenerateEveryModeAtExactLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{"ionian", "C4,D4,E4,F4,G4,A4,B4,C5"},
		{"dorian", "C4,D4,D#4,F4,G4,A4,A#4,C5"},
		{"phrygian", "C4,C#4,D#4,F4,G4,G#4,A#4,C5"},
		{"lydian", "C4,D4,E4,F#4,G4,A4,B4,C5"},
		{"mixolydian", "C4,D4,E4,F4,G4,A4,A#4,C5"},
		{"aeolian", "C4,D4,D#4,F4,G4,G#4,A#4,C5"},
		{"locrian", "C4,C#4,D#4,F4,F#4,G#4,A#4,C5"},
	}
	root, err := ParseNote("C4")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			notes, err := GenerateMode(root, test.name, 8)
			if err != nil {
				t.Fatalf("GenerateMode returned error: %v", err)
			}
			if got := strings.Join(noteNames(notes), ","); got != test.want {
				t.Fatalf("GenerateMode names = %s, want %s", got, test.want)
			}
		})
	}
}

func TestParseScaleAndMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"scale:C4:major:8", "C4,D4,E4,F4,G4,A4,B4,C5"},
		{"scale:A3:minor:12", "A3,B3,C4,D4,E4,F4,G4,A4,B4,C5,D5,E5"},
		{"scale:C3:pentatonic-minor:12", "C3,D#3,F3,G3,A#3,C4,D#4,F4,G4,A#4,C5,D#5"},
		{"mode:E3:phrygian:12", "E3,F3,G3,A3,B3,C4,D4,E4,F4,G4,A4,B4"},
		{"mode:D4:dorian:8", "D4,E4,F4,G4,A4,B4,C5,D5"},
	}
	for _, test := range tests {
		var (
			notes []Note
			err   error
		)
		if strings.HasPrefix(test.input, "scale:") {
			notes, err = ParseScale(test.input)
		} else {
			notes, err = ParseMode(test.input)
		}
		if err != nil {
			t.Fatalf("parse %q returned error: %v", test.input, err)
		}
		if got := strings.Join(noteNames(notes), ","); got != test.want {
			t.Errorf("parse %q = %s, want %s", test.input, got, test.want)
		}
	}
}

func TestGeneratedPrimitiveValidation(t *testing.T) {
	t.Parallel()

	invalidScales := []string{
		"", "scale", "scale:C4:major", "mode:C4:major:8", "scale::major:8",
		"scale:C4::8", "scale:C4:major:", "scale:C4:major:0", "scale:C4:major:-1",
		"scale:C4:major:1.5", "scale:C4:major:2k", "scale:C4:major:8:extra",
		"scale:C4:Major:8", "scale:C4:supermajor:8",
	}
	for _, input := range invalidScales {
		if _, err := ParseScale(input); err == nil {
			t.Errorf("ParseScale(%q) returned nil error", input)
		}
	}

	invalidModes := []string{
		"", "mode", "mode:C4:dorian", "scale:C4:dorian:8", "mode::dorian:8",
		"mode:C4::8", "mode:C4:dorian:", "mode:C4:dorian:0", "mode:C4:dorian:-1",
		"mode:C4:dorian:1.5", "mode:C4:dorian:2k", "mode:C4:dorian:8:extra",
		"mode:C4:Dorian:8", "mode:C4:superlocrian:8",
	}
	for _, input := range invalidModes {
		if _, err := ParseMode(input); err == nil {
			t.Errorf("ParseMode(%q) returned nil error", input)
		}
	}

	root, err := ParseNote("C4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateScale(root, "major", 0); err == nil {
		t.Error("GenerateScale length 0 returned nil error")
	}
	if _, err := GenerateScale(root, "major", int(^uint(0)>>1)); err == nil {
		t.Error("GenerateScale impractically large length returned nil error")
	}
	if _, err := GenerateScale(root, "unknown", 8); err == nil || !strings.Contains(err.Error(), "unknown scale") {
		t.Errorf("GenerateScale unknown error = %v", err)
	}
	if _, err := GenerateMode(root, "unknown", 8); err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("GenerateMode unknown error = %v", err)
	}
}
