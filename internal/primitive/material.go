package primitive

import (
	"fmt"
	"strings"
)

// ParseMaterial parses the note material accepted by -n and the note, scale,
// and mode forms accepted by -p in this implementation slice.
func ParseMaterial(input string) ([]Note, error) {
	switch {
	case strings.HasPrefix(input, "scale:"):
		return ParseScale(input)
	case strings.HasPrefix(input, "mode:"):
		return ParseMode(input)
	default:
		return ParseNoteList(input)
	}
}

// ValidateVectorNotes enforces vector index N to note index N without wrap.
func ValidateVectorNotes(vectorLength, noteCount int) error {
	if vectorLength < 0 {
		return fmt.Errorf("invalid vector length %d: must not be negative", vectorLength)
	}
	if noteCount < 0 {
		return fmt.Errorf("invalid note count %d: must not be negative", noteCount)
	}
	if noteCount < vectorLength {
		return fmt.Errorf("%d vector values require at least %d notes; got %d", vectorLength, vectorLength, noteCount)
	}
	return nil
}

// NotesForVector returns the one-to-one note prefix used by a vector. Extra
// notes are ignored and insufficient note material is rejected.
func NotesForVector(vectorLength int, notes []Note) ([]Note, error) {
	if err := ValidateVectorNotes(vectorLength, len(notes)); err != nil {
		return nil, err
	}
	mapped := make([]Note, vectorLength)
	copy(mapped, notes[:vectorLength])
	return mapped, nil
}
