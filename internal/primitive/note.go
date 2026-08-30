// Package primitive parses and resolves STASH musical primitives.
package primitive

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const a4Number int64 = 69

var (
	notePattern = regexp.MustCompile(`^([A-G])([#b]?)(-?[0-9]+)$`)
	letterPitch = map[byte]int64{
		'C': 0,
		'D': 2,
		'E': 4,
		'F': 5,
		'G': 7,
		'A': 9,
		'B': 11,
	}
	canonicalPitchNames = [...]string{
		"C", "C#", "D", "D#", "E", "F",
		"F#", "G", "G#", "A", "A#", "B",
	}
)

// Note identifies an absolute pitch by its MIDI-compatible semitone number.
// STASH does not restrict notes to the MIDI transmission range.
type Note struct {
	number int64
}

// ParseNote parses scientific pitch notation in LETTER[ACCIDENTAL]OCTAVE form.
func ParseNote(input string) (Note, error) {
	match := notePattern.FindStringSubmatch(input)
	if match == nil {
		return Note{}, fmt.Errorf("invalid note %q: expected LETTER[ACCIDENTAL]OCTAVE", input)
	}

	octave, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil {
		return Note{}, fmt.Errorf("invalid note %q: invalid octave: %w", input, err)
	}

	// Frequencies outside this broad octave window cannot be represented as a
	// finite, positive float64. The bound also keeps semitone arithmetic safe.
	if octave < -2_000 || octave > 2_000 {
		return Note{}, fmt.Errorf("invalid note %q: frequency is not finite and positive", input)
	}

	pitch := letterPitch[match[1][0]]
	switch match[2] {
	case "#":
		pitch++
	case "b":
		pitch--
	}

	number := (octave+1)*12 + pitch
	note, err := noteFromNumber(number)
	if err != nil {
		return Note{}, fmt.Errorf("invalid note %q: %w", input, err)
	}
	return note, nil
}

func noteFromNumber(number int64) (Note, error) {
	frequency := frequencyForNumber(number)
	if frequency <= 0 || math.IsInf(frequency, 0) || math.IsNaN(frequency) {
		return Note{}, fmt.Errorf("frequency is not finite and positive")
	}
	return Note{number: number}, nil
}

func frequencyForNumber(number int64) float64 {
	return 440 * math.Pow(2, (float64(number)-float64(a4Number))/12)
}

// Number returns the note's MIDI-compatible semitone number. A4 is 69 and
// C-1 is 0; values outside the MIDI transmission range remain valid.
func (n Note) Number() int64 {
	return n.number
}

// Frequency resolves the note in twelve-tone equal temperament at A4 = 440 Hz.
func (n Note) Frequency() float64 {
	return frequencyForNumber(n.number)
}

// String returns the canonical sharp spelling for the note.
func (n Note) String() string {
	class := n.number % 12
	if class < 0 {
		class += 12
	}
	octave := (n.number-class)/12 - 1
	return canonicalPitchNames[class] + strconv.FormatInt(octave, 10)
}

// ParseNoteList parses one or more comma-separated scientific-pitch notes.
func ParseNoteList(input string) ([]Note, error) {
	if input == "" {
		return nil, fmt.Errorf("invalid notes %q: expected at least one note", input)
	}

	tokens := strings.Split(input, ",")
	notes := make([]Note, len(tokens))
	for index, token := range tokens {
		if token == "" {
			return nil, fmt.Errorf("invalid notes %q: note %d is empty", input, index+1)
		}
		note, err := ParseNote(token)
		if err != nil {
			return nil, fmt.Errorf("invalid notes %q: note %d: %w", input, index+1, err)
		}
		notes[index] = note
	}
	return notes, nil
}
