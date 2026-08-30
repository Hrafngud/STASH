package primitive

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseMaterialForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  []string
	}{
		{"C4", []string{"C4"}},
		{"C4,E4,G4,C5", []string{"C4", "E4", "G4", "C5"}},
		{"scale:C4:major:3", []string{"C4", "D4", "E4"}},
		{"mode:E3:phrygian:3", []string{"E3", "F3", "G3"}},
	}
	for _, test := range tests {
		notes, err := ParseMaterial(test.input)
		if err != nil {
			t.Fatalf("ParseMaterial(%q) returned error: %v", test.input, err)
		}
		if got := noteNames(notes); !reflect.DeepEqual(got, test.want) {
			t.Errorf("ParseMaterial(%q) = %v, want %v", test.input, got, test.want)
		}
	}

	for _, input := range []string{"", "H4", "rhythm:120:1/8:x-x-", "scale:C4:major:0", "mode:E3:unknown:8"} {
		if _, err := ParseMaterial(input); err == nil {
			t.Errorf("ParseMaterial(%q) returned nil error", input)
		}
	}
}

func TestVectorNoteValidationAndMappingDoesNotWrap(t *testing.T) {
	t.Parallel()

	notes, err := ParseNoteList("C4,D4,E4,G4")
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateVectorNotes(4, 4); err != nil {
		t.Errorf("ValidateVectorNotes equal lengths returned error: %v", err)
	}
	if err := ValidateVectorNotes(2, 4); err != nil {
		t.Errorf("ValidateVectorNotes extra notes returned error: %v", err)
	}
	err = ValidateVectorNotes(5, 4)
	if err == nil || !strings.Contains(err.Error(), "5 vector values require at least 5 notes; got 4") {
		t.Fatalf("ValidateVectorNotes insufficient error = %v", err)
	}

	mapped, err := NotesForVector(3, notes)
	if err != nil {
		t.Fatalf("NotesForVector returned error: %v", err)
	}
	if got, want := noteNames(mapped), []string{"C4", "D4", "E4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NotesForVector = %v, want %v", got, want)
	}
	mapped[0] = notes[3]
	if notes[0].String() != "C4" {
		t.Fatal("NotesForVector returned storage aliased to input")
	}

	if mapped, err := NotesForVector(0, nil); err != nil || len(mapped) != 0 {
		t.Errorf("NotesForVector(0, nil) = %v, %v", mapped, err)
	}
	if _, err := NotesForVector(5, notes); err == nil {
		t.Error("NotesForVector insufficient notes returned nil error")
	}
	if err := ValidateVectorNotes(-1, 0); err == nil {
		t.Error("ValidateVectorNotes negative vector length returned nil error")
	}
	if err := ValidateVectorNotes(0, -1); err == nil {
		t.Error("ValidateVectorNotes negative note count returned nil error")
	}
}
