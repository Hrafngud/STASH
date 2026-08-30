package primitive

import (
	"fmt"
	"strconv"
	"strings"
)

var scaleIntervals = map[string][]int64{
	"major":            {0, 2, 4, 5, 7, 9, 11},
	"minor":            {0, 2, 3, 5, 7, 8, 10},
	"chromatic":        {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	"pentatonic-major": {0, 2, 4, 7, 9},
	"pentatonic-minor": {0, 3, 5, 7, 10},
}

var modeIntervals = map[string][]int64{
	"ionian":     {0, 2, 4, 5, 7, 9, 11},
	"dorian":     {0, 2, 3, 5, 7, 9, 10},
	"phrygian":   {0, 1, 3, 5, 7, 8, 10},
	"lydian":     {0, 2, 4, 6, 7, 9, 11},
	"mixolydian": {0, 2, 4, 5, 7, 9, 10},
	"aeolian":    {0, 2, 3, 5, 7, 8, 10},
	"locrian":    {0, 1, 3, 5, 6, 8, 10},
}

const maxInt64 = int64(^uint64(0) >> 1)

// GenerateScale creates exactly length notes for a documented scale name.
func GenerateScale(root Note, name string, length int) ([]Note, error) {
	intervals, ok := scaleIntervals[name]
	if !ok {
		return nil, fmt.Errorf("unknown scale %q", name)
	}
	return generate(root, intervals, length, "scale")
}

// GenerateMode creates exactly length notes for a documented mode name.
func GenerateMode(root Note, name string, length int) ([]Note, error) {
	intervals, ok := modeIntervals[name]
	if !ok {
		return nil, fmt.Errorf("unknown mode %q", name)
	}
	return generate(root, intervals, length, "mode")
}

func generate(root Note, intervals []int64, length int, family string) ([]Note, error) {
	if length <= 0 {
		return nil, fmt.Errorf("invalid %s length %d: must be a positive integer", family, length)
	}
	// Validate the highest requested pitch before allocating. Since every
	// documented interval sequence ascends, this also validates every earlier
	// generated note and rejects impractically large lengths safely.
	lastNumber, err := generatedNumber(root, intervals, length-1)
	if err != nil {
		return nil, fmt.Errorf("invalid %s length %d: %w", family, length, err)
	}
	if _, err := noteFromNumber(lastNumber); err != nil {
		return nil, fmt.Errorf("invalid %s length %d: generated note %d: %w", family, length, length, err)
	}

	notes := make([]Note, length)
	for index := range notes {
		number, err := generatedNumber(root, intervals, index)
		if err != nil {
			return nil, fmt.Errorf("invalid %s length %d: %w", family, length, err)
		}
		note, err := noteFromNumber(number)
		if err != nil {
			return nil, fmt.Errorf("invalid %s length %d: generated note %d: %w", family, length, index+1, err)
		}
		notes[index] = note
	}
	return notes, nil
}

func generatedNumber(root Note, intervals []int64, index int) (int64, error) {
	cycles := int64(index / len(intervals))
	interval := intervals[index%len(intervals)]
	if cycles > (maxInt64-interval)/12 {
		return 0, fmt.Errorf("generated pitch overflows")
	}
	offset := cycles*12 + interval
	if root.number > maxInt64-offset {
		return 0, fmt.Errorf("generated pitch overflows")
	}
	return root.number + offset, nil
}

// ParseScale parses scale:ROOT:NAME:LENGTH and resolves its notes.
func ParseScale(input string) ([]Note, error) {
	return parseGenerated(input, "scale", GenerateScale)
}

// ParseMode parses mode:ROOT:NAME:LENGTH and resolves its notes.
func ParseMode(input string) ([]Note, error) {
	return parseGenerated(input, "mode", GenerateMode)
}

func parseGenerated(input, family string, generator func(Note, string, int) ([]Note, error)) ([]Note, error) {
	parts := strings.Split(input, ":")
	if len(parts) != 4 || parts[0] != family {
		return nil, fmt.Errorf("invalid %s %q: expected %s:ROOT:NAME:LENGTH", family, input, family)
	}

	root, err := ParseNote(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q: root: %w", family, input, err)
	}
	if parts[2] == "" {
		return nil, fmt.Errorf("invalid %s %q: name is empty", family, input)
	}
	length, err := parseLength(parts[3])
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q: %w", family, input, err)
	}

	notes, err := generator(root, parts[2], length)
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q: %w", family, input, err)
	}
	return notes, nil
}

func parseLength(input string) (int, error) {
	if input == "" {
		return 0, fmt.Errorf("length is empty")
	}
	for _, character := range input {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("invalid length %q: must be a positive integer", input)
		}
	}
	length, err := strconv.Atoi(input)
	if err != nil || length <= 0 {
		if err != nil {
			return 0, fmt.Errorf("invalid length %q: must be a positive integer: %w", input, err)
		}
		return 0, fmt.Errorf("invalid length %q: must be a positive integer", input)
	}
	return length, nil
}
