package control

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

func TestParseMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Mapping
	}{
		{"80..2k", Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveLinear}},
		{"80..2k/linear", Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveLinear}},
		{"80..2k/exp", Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveExp}},
		{"80..2k/log", Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveLog}},
		{"80..2k~150ms", Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveLinear, Smoothing: 150 * time.Millisecond}},
		{"80..2k/exp~1.5s", Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveExp, Smoothing: 1500 * time.Millisecond}},
		{"-1..1/log~0ms", Mapping{Output: unit.Range{Min: -1, Max: 1}, Curve: CurveLog}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMapping(test.input)
			if err != nil {
				t.Fatalf("ParseMapping(%q) returned error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseMapping(%q) = %#v, want %#v", test.input, got, test.want)
			}
		})
	}
}

func TestParseMappingRejectsMalformedExpressions(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"", "80", "80...2k", "2k..80", "80..2k/", "/linear",
		"80..2k/cubic", "80..2k/EXP", "80..2k/exp/log",
		"80..2k~", "~1s", "80..2k~-1s", "80..2k~1", "80..2k~1s~2s",
		"80..2k~1s/exp", " 80..2k", "80..2k ",
	}
	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseMapping(input)
			if err == nil {
				t.Fatalf("ParseMapping(%q) returned no error", input)
			}
			if !strings.Contains(err.Error(), "invalid mapping") {
				t.Fatalf("ParseMapping(%q) error = %q, want mapping context", input, err)
			}
		})
	}
}

func TestNormalizeClampsInput(t *testing.T) {
	t.Parallel()

	input := unit.Range{Min: 20, Max: 120}
	tests := []struct {
		value float64
		want  float64
	}{
		{-100, 0},
		{20, 0},
		{45, 0.25},
		{120, 1},
		{1_000, 1},
	}
	for _, test := range tests {
		got, err := Normalize(test.value, input)
		if err != nil {
			t.Fatalf("Normalize(%v, %#v) returned error: %v", test.value, input, err)
		}
		if got != test.want {
			t.Errorf("Normalize(%v, %#v) = %v, want %v", test.value, input, got, test.want)
		}
	}
}

func TestCurvesHaveDeterministicEndpointsAndShape(t *testing.T) {
	t.Parallel()

	for _, curve := range []Curve{CurveLinear, CurveExp, CurveLog} {
		low, err := ApplyCurve(0, curve)
		if err != nil {
			t.Fatalf("ApplyCurve(0, %q) returned error: %v", curve, err)
		}
		high, err := ApplyCurve(1, curve)
		if err != nil {
			t.Fatalf("ApplyCurve(1, %q) returned error: %v", curve, err)
		}
		assertClose(t, low, 0, 1e-15)
		assertClose(t, high, 1, 1e-15)
	}

	linear, _ := ApplyCurve(0.5, CurveLinear)
	exponential, _ := ApplyCurve(0.5, CurveExp)
	logarithmic, _ := ApplyCurve(0.5, CurveLog)
	if !(exponential < linear && linear < logarithmic) {
		t.Fatalf("curve midpoint order = exp %v, linear %v, log %v", exponential, linear, logarithmic)
	}
	roundTrip, err := ApplyCurve(exponential, CurveLog)
	if err != nil {
		t.Fatalf("ApplyCurve(exp(.5), log) returned error: %v", err)
	}
	assertClose(t, roundTrip, 0.5, 1e-15)
}

func TestMappingTransformOrderAndClamping(t *testing.T) {
	t.Parallel()

	mapping := Mapping{Output: unit.Range{Min: 80, Max: 2_000}, Curve: CurveExp}
	input := unit.Range{Min: 0, Max: 100}

	low, err := mapping.Transform(-1, input)
	if err != nil {
		t.Fatalf("Transform below input range returned error: %v", err)
	}
	high, err := mapping.Transform(101, input)
	if err != nil {
		t.Fatalf("Transform above input range returned error: %v", err)
	}
	mid, err := mapping.Transform(50, input)
	if err != nil {
		t.Fatalf("Transform midpoint returned error: %v", err)
	}

	assertClose(t, low, 80, 1e-12)
	assertClose(t, high, 2_000, 1e-12)
	wantMid := 80 + (math.Expm1(0.5)/math.Expm1(1))*(2_000-80)
	assertClose(t, mid, wantMid, 1e-12)
}

func TestMapperSmoothsAfterOutputInterpolation(t *testing.T) {
	t.Parallel()

	mapping := Mapping{
		Output:    unit.Range{Min: 100, Max: 1_100},
		Curve:     CurveLinear,
		Smoothing: time.Second,
	}
	mapper, err := NewMapper(mapping, unit.Range{Min: 0, Max: 100}, 100)
	if err != nil {
		t.Fatalf("NewMapper returned error: %v", err)
	}
	got, err := mapper.Step(50, time.Second)
	if err != nil {
		t.Fatalf("Step returned error: %v", err)
	}
	// 50 first maps to 600, then the smoother advances from 100 toward 600.
	want := 100 + (1-math.Exp(-1))*(600-100)
	assertClose(t, got, want, 1e-12)
	assertClose(t, mapper.Value(), want, 1e-12)
}

func TestNewMapperValidatesConfiguration(t *testing.T) {
	t.Parallel()

	valid := Mapping{Output: unit.Range{Min: 0, Max: 1}, Curve: CurveLinear}
	if _, err := NewMapper(valid, unit.Range{Min: 1, Max: 1}, 0); err == nil {
		t.Error("NewMapper accepted an invalid input range")
	}
	invalidOutput := Mapping{Output: unit.Range{Min: 1, Max: 1}, Curve: CurveLinear}
	if _, err := NewMapper(invalidOutput, unit.Range{Min: 0, Max: 1}, 0); err == nil {
		t.Error("NewMapper accepted an invalid output range")
	}
	invalidCurve := Mapping{Output: unit.Range{Min: 0, Max: 1}, Curve: Curve("cubic")}
	if _, err := NewMapper(invalidCurve, unit.Range{Min: 0, Max: 1}, 0); err == nil {
		t.Error("NewMapper accepted an invalid curve")
	}
	invalidSmoothing := Mapping{Output: unit.Range{Min: 0, Max: 1}, Curve: CurveLinear, Smoothing: -time.Millisecond}
	if _, err := NewMapper(invalidSmoothing, unit.Range{Min: 0, Max: 1}, 0); err == nil {
		t.Error("NewMapper accepted a negative smoothing duration")
	}
}

func TestMappingOperationsRejectInvalidRuntimeValues(t *testing.T) {
	t.Parallel()

	invalidRange := unit.Range{Min: 1, Max: 1}
	validRange := unit.Range{Min: 0, Max: 1}
	if _, err := Normalize(0.5, invalidRange); err == nil {
		t.Error("Normalize accepted a non-increasing range")
	}
	if _, err := Normalize(math.NaN(), validRange); err == nil {
		t.Error("Normalize accepted NaN")
	}
	if _, err := ApplyCurve(0.5, Curve("cubic")); err == nil {
		t.Error("ApplyCurve accepted an unknown curve")
	}
	if _, err := Interpolate(math.Inf(1), validRange); err == nil {
		t.Error("Interpolate accepted infinity")
	}
	overflowingRange := unit.Range{Min: -math.MaxFloat64, Max: math.MaxFloat64}
	if _, err := Normalize(0, overflowingRange); err == nil {
		t.Error("Normalize accepted a non-finite range span")
	}
}

func assertClose(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("got %.17g, want %.17g (tolerance %g)", got, want, tolerance)
	}
}
