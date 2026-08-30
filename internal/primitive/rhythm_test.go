package primitive

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseRhythm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input     string
		bpm       float64
		hasBPM    bool
		division  Division
		pattern   string
		stepCount int
	}{
		{"rhythm:120:1/4:xxxx", 120, true, DivisionQuarter, "xxxx", 4},
		{"rhythm:172.5:1/16:x---x---x-x-x---", 172.5, true, DivisionSixteenth, "x---x---x-x-x---", 16},
		{"rhythm:1/8:x-x-x-x-", 0, false, DivisionEighth, "x-x-x-x-", 8},
		{"rhythm:.5:1/32:-", .5, true, DivisionThirtySecond, "-", 1},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			rhythm, err := ParseRhythm(test.input)
			if err != nil {
				t.Fatalf("ParseRhythm(%q) error = %v", test.input, err)
			}
			bpm, hasBPM := rhythm.EmbeddedBPM()
			if bpm != test.bpm || hasBPM != test.hasBPM {
				t.Errorf("EmbeddedBPM() = (%v, %t), want (%v, %t)", bpm, hasBPM, test.bpm, test.hasBPM)
			}
			if rhythm.Division != test.division || rhythm.Pattern != test.pattern || rhythm.StepCount() != test.stepCount {
				t.Errorf("rhythm = {%s %q %d}, want {%s %q %d}", rhythm.Division, rhythm.Pattern, rhythm.StepCount(), test.division, test.pattern, test.stepCount)
			}
		})
	}
}

func TestParseRhythmRejectsMalformedForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"", "expected rhythm"},
		{"beat:120:1/8:x", "expected rhythm"},
		{"rhythm", "expected rhythm"},
		{"rhythm:120:1/8", "invalid rhythm division"},
		{"rhythm::1/8:x", "embedded BPM is empty"},
		{"rhythm:0:1/8:x", "must be greater than zero"},
		{"rhythm:-1:1/8:x", "must be greater than zero"},
		{"rhythm:nan:1/8:x", "invalid number"},
		{"rhythm:120:1/3:x", "invalid rhythm division"},
		{"rhythm:120:1/08:x", "invalid rhythm division"},
		{"rhythm:120:1/8:", "expected at least one step"},
		{"rhythm:120:1/8:x_o_x", "unsupported symbol"},
		{"rhythm:120:1/8:x:extra", "expected rhythm"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseRhythm(test.input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseRhythm(%q) error = %v, want containing %q", test.input, err, test.want)
			}
		})
	}
}

func TestDivisionAndNumericValidation(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]Division{
		"1/1":  DivisionWhole,
		"1/2":  DivisionHalf,
		"1/4":  DivisionQuarter,
		"1/8":  DivisionEighth,
		"1/16": DivisionSixteenth,
		"1/32": DivisionThirtySecond,
	} {
		got, err := ParseDivision(input)
		if err != nil || got != want || got.String() != input {
			t.Errorf("ParseDivision(%q) = (%v, %v), want (%v, nil)", input, got, err, want)
		}
	}

	for _, input := range []string{"90", "120", "172.5", ".5", "1k"} {
		if _, err := ParseBPM(input); err != nil {
			t.Errorf("ParseBPM(%q) error = %v", input, err)
		}
	}
	for _, input := range []string{"", "0", "-1", "NaN", "120bpm"} {
		if _, err := ParseBPM(input); err == nil {
			t.Errorf("ParseBPM(%q) returned nil error", input)
		}
	}

	for _, input := range []string{"50", "58", "75", "50.5"} {
		if _, err := ParseSwing(input); err != nil {
			t.Errorf("ParseSwing(%q) error = %v", input, err)
		}
	}
	for _, input := range []string{"", "49.999", "75.001", "-50", "NaN"} {
		if _, err := ParseSwing(input); err == nil {
			t.Errorf("ParseSwing(%q) returned nil error", input)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := ValidateSwing(value); err == nil {
			t.Errorf("ValidateSwing(%v) returned nil error", value)
		}
	}
}

func TestRhythmTempoResolutionAndPattern(t *testing.T) {
	t.Parallel()

	embedded, err := ParseRhythm("rhythm:120:1/8:x-")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := embedded.ResolveBPM(nil); err != nil || got != 120 {
		t.Fatalf("ResolveBPM(nil) = (%v, %v), want (120, nil)", got, err)
	}
	override := 90.0
	if got, err := embedded.ResolveBPM(&override); err != nil || got != override {
		t.Fatalf("ResolveBPM(&90) = (%v, %v), want (90, nil)", got, err)
	}

	omitted, err := ParseRhythm("rhythm:1/8:x-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := omitted.ResolveBPM(nil); err == nil {
		t.Fatal("ResolveBPM(nil) for omitted tempo returned nil error")
	}
	invalid := []float64{0, -1, math.NaN(), math.Inf(1)}
	for _, value := range invalid {
		if _, err := omitted.ResolveBPM(&value); err == nil {
			t.Errorf("ResolveBPM(%v) returned nil error", value)
		}
	}

	wantHits := []bool{true, false, true, false, true}
	for step, want := range wantHits {
		got, err := omitted.IsHit(int64(step))
		if err != nil || got != want {
			t.Errorf("IsHit(%d) = (%t, %v), want (%t, nil)", step, got, err, want)
		}
	}
	if _, err := omitted.IsHit(-1); err == nil {
		t.Fatal("IsHit(-1) returned nil error")
	}
}

func TestRhythmClockStraightControlsAndHitPulse(t *testing.T) {
	t.Parallel()

	rhythm, err := ParseRhythm("rhythm:120:1/8:x-")
	if err != nil {
		t.Fatal(err)
	}
	origin := time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC)
	clock, err := NewRhythmClock(rhythm, nil, DefaultSwing, origin)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		offset   time.Duration
		gate     float64
		hit      float64
		step     int
		velocity float64
		phase    float64
	}{
		{0, 1, 1, 0, 1, 0},
		{125 * time.Millisecond, 1, 0, 0, 1, .5},
		{250 * time.Millisecond, 0, 0, 1, 0, 0},
		{375 * time.Millisecond, 0, 0, 1, 0, .5},
		{500 * time.Millisecond, 1, 1, 0, 1, 0},
		{625 * time.Millisecond, 1, 0, 0, 1, .5},
	}

	for _, test := range tests {
		controls, err := clock.Evaluate(origin.Add(test.offset))
		if err != nil {
			t.Fatalf("Evaluate(%s) error = %v", test.offset, err)
		}
		if controls.Gate != test.gate || controls.Hit != test.hit || controls.Step != test.step || controls.Velocity != test.velocity || math.Abs(controls.Phase-test.phase) > 1e-12 {
			t.Errorf("Evaluate(%s) = %+v, want gate=%v hit=%v step=%d velocity=%v phase=%v", test.offset, controls, test.gate, test.hit, test.step, test.velocity, test.phase)
		}
	}
}

func TestRhythmClockSwingAndOriginBasedProgression(t *testing.T) {
	t.Parallel()

	rhythm, err := ParseRhythm("rhythm:120:1/8:xxxx")
	if err != nil {
		t.Fatal(err)
	}
	origin := time.Unix(0, 0)
	clock, err := NewRhythmClock(rhythm, nil, 60, origin)
	if err != nil {
		t.Fatal(err)
	}

	// Straight eighths at 120 BPM are 250ms. A 60% swing divides each
	// 500ms pair into a 300ms long step and a 200ms short step.
	tests := []struct {
		offset time.Duration
		step   int
		phase  float64
	}{
		{0, 0, 0},
		{150 * time.Millisecond, 0, .5},
		{300 * time.Millisecond, 1, 0},
		{400 * time.Millisecond, 1, .5},
		{500 * time.Millisecond, 2, 0},
		{800 * time.Millisecond, 3, 0},
		{time.Second, 0, 0},
	}
	for _, test := range tests {
		controls, err := clock.Evaluate(origin.Add(test.offset))
		if err != nil {
			t.Fatalf("Evaluate(%s) error = %v", test.offset, err)
		}
		if controls.Step != test.step || math.Abs(controls.Phase-test.phase) > 1e-12 {
			t.Errorf("Evaluate(%s) = step %d phase %v, want step %d phase %v", test.offset, controls.Step, controls.Phase, test.step, test.phase)
		}
	}

	// A late observation does not move the next target: it remains the
	// boundary derived from the fixed origin.
	next, err := clock.NextStepTime(origin.Add(1*time.Second + 275*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	want := origin.Add(1*time.Second + 300*time.Millisecond)
	if !next.Equal(want) {
		t.Errorf("NextStepTime(late) = %s, want %s", next, want)
	}
}

func TestRhythmClockRejectsInvalidTimeAndConfiguration(t *testing.T) {
	t.Parallel()

	omitted, err := ParseRhythm("rhythm:1/8:x")
	if err != nil {
		t.Fatal(err)
	}
	origin := time.Unix(100, 0)
	if _, err := NewRhythmClock(omitted, nil, DefaultSwing, origin); err == nil {
		t.Fatal("NewRhythmClock without BPM returned nil error")
	}
	bpm := 120.0
	if _, err := NewRhythmClock(omitted, &bpm, 49, origin); err == nil {
		t.Fatal("NewRhythmClock with invalid swing returned nil error")
	}
	hugeBPM := float64(4*time.Minute) * 2
	if _, err := NewRhythmClock(omitted, &hugeBPM, DefaultSwing, origin); err == nil {
		t.Fatal("NewRhythmClock with sub-nanosecond steps returned nil error")
	}

	clock, err := NewRhythmClock(omitted, &bpm, DefaultSwing, origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clock.Evaluate(origin.Add(-time.Nanosecond)); err == nil {
		t.Fatal("Evaluate(before origin) returned nil error")
	}
	if _, err := clock.NextStepTime(origin.Add(-time.Nanosecond)); err == nil {
		t.Fatal("NextStepTime(before origin) returned nil error")
	}
	if _, err := clock.Evaluate(origin.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := clock.Evaluate(origin.Add(500 * time.Millisecond)); err == nil {
		t.Fatal("Evaluate(backwards) returned nil error")
	}
}
