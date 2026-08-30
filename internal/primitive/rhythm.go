package primitive

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

const (
	// DefaultSwing is straight timing, expressed as the first step's
	// percentage of an adjacent pair of subdivisions.
	DefaultSwing = 50.0
	minSwing     = 50.0
	maxSwing     = 75.0
)

// Division identifies the duration represented by one rhythm pattern step.
// Its value is the denominator in the documented 1/N syntax.
type Division uint8

const (
	DivisionWhole        Division = 1
	DivisionHalf         Division = 2
	DivisionQuarter      Division = 4
	DivisionEighth       Division = 8
	DivisionSixteenth    Division = 16
	DivisionThirtySecond Division = 32
)

// ParseDivision parses one of the divisions supported by the initial rhythm
// grammar.
func ParseDivision(input string) (Division, error) {
	switch input {
	case "1/1":
		return DivisionWhole, nil
	case "1/2":
		return DivisionHalf, nil
	case "1/4":
		return DivisionQuarter, nil
	case "1/8":
		return DivisionEighth, nil
	case "1/16":
		return DivisionSixteenth, nil
	case "1/32":
		return DivisionThirtySecond, nil
	default:
		return 0, fmt.Errorf("invalid rhythm division %q: expected 1/1, 1/2, 1/4, 1/8, 1/16, or 1/32", input)
	}
}

func (division Division) String() string {
	switch division {
	case DivisionWhole, DivisionHalf, DivisionQuarter, DivisionEighth, DivisionSixteenth, DivisionThirtySecond:
		return fmt.Sprintf("1/%d", division)
	default:
		return fmt.Sprintf("Division(%d)", division)
	}
}

// ParseBPM parses and validates a strictly positive finite tempo.
func ParseBPM(input string) (float64, error) {
	bpm, err := unit.ParseNumber(input)
	if err != nil {
		return 0, fmt.Errorf("invalid BPM %q: %w", input, err)
	}
	if bpm <= 0 {
		return 0, fmt.Errorf("invalid BPM %q: must be greater than zero", input)
	}
	return bpm, nil
}

// ParseSwing parses a swing percentage in the inclusive 50..75 range.
func ParseSwing(input string) (float64, error) {
	swing, err := unit.ParseNumber(input)
	if err != nil {
		return 0, fmt.Errorf("invalid swing %q: %w", input, err)
	}
	if err := ValidateSwing(swing); err != nil {
		return 0, fmt.Errorf("invalid swing %q: %w", input, err)
	}
	return swing, nil
}

// ValidateSwing validates a parsed swing percentage.
func ValidateSwing(swing float64) error {
	if math.IsNaN(swing) || math.IsInf(swing, 0) {
		return fmt.Errorf("value must be finite")
	}
	if swing < minSwing || swing > maxSwing {
		return fmt.Errorf("value must be between 50 and 75")
	}
	return nil
}

// Rhythm is a validated rhythm primitive. An omitted embedded tempo is kept
// distinct from zero so a later planner can require or apply -b precisely.
type Rhythm struct {
	Division Division
	Pattern  string

	embeddedBPM float64
	hasBPM      bool
}

// ParseRhythm parses rhythm:BPM:DIVISION:PATTERN and
// rhythm:DIVISION:PATTERN.
func ParseRhythm(input string) (Rhythm, error) {
	parts := strings.Split(input, ":")
	if len(parts) < 1 || parts[0] != "rhythm" {
		return Rhythm{}, fmt.Errorf("invalid rhythm %q: expected rhythm:BPM:DIVISION:PATTERN or rhythm:DIVISION:PATTERN", input)
	}

	var bpmToken, divisionToken, pattern string
	switch len(parts) {
	case 4:
		bpmToken, divisionToken, pattern = parts[1], parts[2], parts[3]
	case 3:
		divisionToken, pattern = parts[1], parts[2]
	default:
		return Rhythm{}, fmt.Errorf("invalid rhythm %q: expected rhythm:BPM:DIVISION:PATTERN or rhythm:DIVISION:PATTERN", input)
	}

	division, err := ParseDivision(divisionToken)
	if err != nil {
		return Rhythm{}, fmt.Errorf("invalid rhythm %q: %w", input, err)
	}
	if err := validatePattern(pattern); err != nil {
		return Rhythm{}, fmt.Errorf("invalid rhythm %q: %w", input, err)
	}

	rhythm := Rhythm{Division: division, Pattern: pattern}
	if bpmToken != "" {
		bpm, err := ParseBPM(bpmToken)
		if err != nil {
			return Rhythm{}, fmt.Errorf("invalid rhythm %q: %w", input, err)
		}
		rhythm.embeddedBPM = bpm
		rhythm.hasBPM = true
	} else if len(parts) == 4 {
		return Rhythm{}, fmt.Errorf("invalid rhythm %q: embedded BPM is empty", input)
	}
	return rhythm, nil
}

func validatePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("invalid rhythm pattern %q: expected at least one step", pattern)
	}
	for index, symbol := range pattern {
		if symbol != 'x' && symbol != '-' {
			return fmt.Errorf("invalid rhythm pattern %q: step %d has unsupported symbol %q", pattern, index, symbol)
		}
	}
	return nil
}

// EmbeddedBPM returns the primitive's embedded tempo and whether it was
// present.
func (rhythm Rhythm) EmbeddedBPM() (float64, bool) {
	return rhythm.embeddedBPM, rhythm.hasBPM
}

// ResolveBPM applies a provided -b-style override, otherwise uses the
// embedded tempo. A tempo-omitted primitive without an override is invalid.
func (rhythm Rhythm) ResolveBPM(override *float64) (float64, error) {
	if override != nil {
		if math.IsNaN(*override) || math.IsInf(*override, 0) || *override <= 0 {
			return 0, fmt.Errorf("invalid BPM %v: must be finite and greater than zero", *override)
		}
		return *override, nil
	}
	if rhythm.hasBPM {
		return rhythm.embeddedBPM, nil
	}
	return 0, fmt.Errorf("rhythm %s:%s has no BPM; provide -b BPM", rhythm.Division, rhythm.Pattern)
}

// StepCount returns the number of steps in the pattern.
func (rhythm Rhythm) StepCount() int {
	return len(rhythm.Pattern)
}

// IsHit reports whether the zero-based pattern step is a hit. Pattern steps
// wrap because rhythms repeat; negative indexes are rejected.
func (rhythm Rhythm) IsHit(step int64) (bool, error) {
	if step < 0 {
		return false, fmt.Errorf("invalid rhythm step %d: must be non-negative", step)
	}
	if rhythm.Pattern == "" {
		return false, fmt.Errorf("invalid rhythm: pattern is empty")
	}
	return rhythm.Pattern[step%int64(len(rhythm.Pattern))] == 'x', nil
}

// RhythmControls is one deterministic evaluation of the five documented
// rhythm controls.
type RhythmControls struct {
	Gate     float64
	Hit      float64
	Step     int
	Velocity float64
	Phase    float64
}

// RhythmClock evaluates rhythm controls from a fixed origin. It never advances
// by adding sleeps, so late evaluations cannot shift future step boundaries.
type RhythmClock struct {
	rhythm Rhythm
	origin time.Time
	long   time.Duration
	short  time.Duration

	evaluated   bool
	lastAt      time.Time
	lastAbsStep int64
}

// NewRhythmClock constructs an origin-based clock. overrideBPM represents a
// -b value and takes precedence over an embedded rhythm BPM. A nil override
// uses the embedded BPM. Swing is an already parsed percentage.
func NewRhythmClock(rhythm Rhythm, overrideBPM *float64, swing float64, origin time.Time) (*RhythmClock, error) {
	if rhythm.Pattern == "" {
		return nil, fmt.Errorf("invalid rhythm: pattern is empty")
	}
	if err := ValidateSwing(swing); err != nil {
		return nil, fmt.Errorf("invalid swing: %w", err)
	}
	bpm, err := rhythm.ResolveBPM(overrideBPM)
	if err != nil {
		return nil, err
	}

	baseNanos := float64(4*time.Minute) / (bpm * float64(rhythm.Division))
	if math.IsNaN(baseNanos) || math.IsInf(baseNanos, 0) || baseNanos < 1 || baseNanos > float64(math.MaxInt64/2) {
		return nil, fmt.Errorf("invalid BPM %v for division %s: step duration is outside the supported clock range", bpm, rhythm.Division)
	}
	base := time.Duration(math.Round(baseNanos))
	pair := 2 * base
	long := time.Duration(math.Round(float64(pair) * swing / 100))
	short := pair - long
	if long <= 0 || short <= 0 {
		return nil, fmt.Errorf("invalid rhythm timing: step duration must be positive")
	}

	return &RhythmClock{
		rhythm: rhythm,
		origin: origin,
		long:   long,
		short:  short,
	}, nil
}

// Evaluate returns controls at an injected timestamp. Calls must be
// monotonic. Hit is a one-evaluation pulse on the first observation of each
// hit step, including the initial observation.
func (clock *RhythmClock) Evaluate(at time.Time) (RhythmControls, error) {
	absStep, phase, err := clock.position(at)
	if err != nil {
		return RhythmControls{}, err
	}
	if clock.evaluated && at.Before(clock.lastAt) {
		return RhythmControls{}, fmt.Errorf("rhythm clock moved backwards from %s to %s", clock.lastAt.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	}

	hit, err := clock.rhythm.IsHit(absStep)
	if err != nil {
		return RhythmControls{}, err
	}
	level := 0.0
	if hit {
		level = 1
	}
	pulse := 0.0
	if hit && (!clock.evaluated || absStep != clock.lastAbsStep) {
		pulse = 1
	}

	clock.evaluated = true
	clock.lastAt = at
	clock.lastAbsStep = absStep
	return RhythmControls{
		Gate:     level,
		Hit:      pulse,
		Step:     int(absStep % int64(clock.rhythm.StepCount())),
		Velocity: level,
		Phase:    phase,
	}, nil
}

// NextStepTime returns the next step boundary strictly after at. It is always
// calculated from the original clock origin.
func (clock *RhythmClock) NextStepTime(at time.Time) (time.Time, error) {
	absStep, _, err := clock.position(at)
	if err != nil {
		return time.Time{}, err
	}
	pairIndex := absStep / 2
	pairOffset := time.Duration(pairIndex) * (clock.long + clock.short)
	if absStep%2 == 0 {
		return clock.origin.Add(pairOffset + clock.long), nil
	}
	return clock.origin.Add(pairOffset + clock.long + clock.short), nil
}

func (clock *RhythmClock) position(at time.Time) (int64, float64, error) {
	if at.Before(clock.origin) {
		return 0, 0, fmt.Errorf("rhythm timestamp %s precedes origin %s", at.Format(time.RFC3339Nano), clock.origin.Format(time.RFC3339Nano))
	}
	elapsed := at.Sub(clock.origin)
	pair := clock.long + clock.short
	pairIndex := int64(elapsed / pair)
	withinPair := elapsed % pair
	if withinPair < clock.long {
		return pairIndex * 2, float64(withinPair) / float64(clock.long), nil
	}
	return pairIndex*2 + 1, float64(withinPair-clock.long) / float64(clock.short), nil
}
