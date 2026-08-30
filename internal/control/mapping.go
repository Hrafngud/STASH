// Package control implements the deterministic control transformations used by
// STASH mappings and triggers.
package control

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

// Curve identifies a mapping curve over the normalized interval [0, 1].
type Curve string

const (
	CurveLinear Curve = "linear"
	CurveExp    Curve = "exp"
	CurveLog    Curve = "log"
)

// Mapping describes an output range, curve, and smoothing time constant.
type Mapping struct {
	Output    unit.Range
	Curve     Curve
	Smoothing time.Duration
}

// Mapper applies a Mapping to one input stream and retains its smoothed output.
type Mapper struct {
	mapping  Mapping
	input    unit.Range
	smoother *Smoother
}

// ParseMapping parses MIN..MAX[/CURVE][~SMOOTH].
func ParseMapping(input string) (Mapping, error) {
	if input == "" {
		return Mapping{}, fmt.Errorf("invalid mapping %q: expected MIN..MAX[/CURVE][~SMOOTH]", input)
	}
	if strings.Count(input, "~") > 1 {
		return Mapping{}, fmt.Errorf("invalid mapping %q: expected at most one smoothing separator", input)
	}

	mappingPart := input
	smoothing := time.Duration(0)
	if before, after, found := strings.Cut(input, "~"); found {
		if before == "" || after == "" {
			return Mapping{}, fmt.Errorf("invalid mapping %q: expected mapping and smoothing duration", input)
		}
		mappingPart = before
		parsed, err := unit.ParseDuration(after)
		if err != nil {
			return Mapping{}, fmt.Errorf("invalid mapping %q: smoothing: %w", input, err)
		}
		smoothing = parsed
	}

	if strings.Count(mappingPart, "/") > 1 {
		return Mapping{}, fmt.Errorf("invalid mapping %q: expected at most one curve separator", input)
	}
	rangePart := mappingPart
	curve := CurveLinear
	if before, after, found := strings.Cut(mappingPart, "/"); found {
		if before == "" || after == "" {
			return Mapping{}, fmt.Errorf("invalid mapping %q: expected range and curve", input)
		}
		rangePart = before
		parsed, err := ParseCurve(after)
		if err != nil {
			return Mapping{}, fmt.Errorf("invalid mapping %q: %w", input, err)
		}
		curve = parsed
	}
	if strings.Contains(rangePart, "...") {
		return Mapping{}, fmt.Errorf("invalid mapping %q: output range: expected MIN..MAX", input)
	}

	output, err := unit.ParseRange(rangePart)
	if err != nil {
		return Mapping{}, fmt.Errorf("invalid mapping %q: output range: %w", input, err)
	}
	return Mapping{Output: output, Curve: curve, Smoothing: smoothing}, nil
}

// ParseCurve parses one of the three public mapping curve names.
func ParseCurve(input string) (Curve, error) {
	curve := Curve(input)
	switch curve {
	case CurveLinear, CurveExp, CurveLog:
		return curve, nil
	default:
		return "", fmt.Errorf("unknown curve %q: expected linear, exp, or log", input)
	}
}

// Normalize clamps value to input and normalizes it to [0, 1].
func Normalize(value float64, input unit.Range) (float64, error) {
	if err := validateFinite("input value", value); err != nil {
		return 0, err
	}
	if err := validateRange("input range", input); err != nil {
		return 0, err
	}
	if value <= input.Min {
		return 0, nil
	}
	if value >= input.Max {
		return 1, nil
	}
	return (value - input.Min) / (input.Max - input.Min), nil
}

// ApplyCurve transforms a normalized value. Values outside [0, 1] are
// clamped, so the function is safe to use independently of Normalize.
func ApplyCurve(value float64, curve Curve) (float64, error) {
	if err := validateFinite("normalized value", value); err != nil {
		return 0, err
	}
	value = clamp01(value)
	switch curve {
	case CurveLinear:
		return value, nil
	case CurveExp:
		return math.Expm1(value) / math.Expm1(1), nil
	case CurveLog:
		return math.Log1p(math.Expm1(1) * value), nil
	default:
		return 0, fmt.Errorf("unknown curve %q", curve)
	}
}

// Interpolate maps a normalized value into output. Values outside [0, 1] are
// clamped.
func Interpolate(value float64, output unit.Range) (float64, error) {
	if err := validateFinite("normalized value", value); err != nil {
		return 0, err
	}
	if err := validateRange("output range", output); err != nil {
		return 0, err
	}
	value = clamp01(value)
	return output.Min + value*(output.Max-output.Min), nil
}

// Transform applies the mapping stages before smoothing: normalize and clamp,
// curve, then interpolate into the output range.
func (mapping Mapping) Transform(value float64, input unit.Range) (float64, error) {
	normalized, err := Normalize(value, input)
	if err != nil {
		return 0, fmt.Errorf("normalize mapping input: %w", err)
	}
	curved, err := ApplyCurve(normalized, mapping.Curve)
	if err != nil {
		return 0, fmt.Errorf("apply mapping curve: %w", err)
	}
	output, err := Interpolate(curved, mapping.Output)
	if err != nil {
		return 0, fmt.Errorf("interpolate mapping output: %w", err)
	}
	return output, nil
}

// NewMapper constructs the complete normalize, curve, interpolate, and smooth
// pipeline. initialOutput is the target's value before the first update.
func NewMapper(mapping Mapping, input unit.Range, initialOutput float64) (*Mapper, error) {
	if err := validateRange("input range", input); err != nil {
		return nil, err
	}
	if err := validateRange("output range", mapping.Output); err != nil {
		return nil, err
	}
	if _, err := ApplyCurve(0, mapping.Curve); err != nil {
		return nil, err
	}
	smoother, err := NewSmoother(mapping.Smoothing, initialOutput)
	if err != nil {
		return nil, fmt.Errorf("construct mapping smoother: %w", err)
	}
	return &Mapper{mapping: mapping, input: input, smoother: smoother}, nil
}

// Step maps value and then smooths the mapped output over delta.
func (mapper *Mapper) Step(value float64, delta time.Duration) (float64, error) {
	target, err := mapper.mapping.Transform(value, mapper.input)
	if err != nil {
		return 0, err
	}
	output, err := mapper.smoother.Step(target, delta)
	if err != nil {
		return 0, fmt.Errorf("smooth mapping output: %w", err)
	}
	return output, nil
}

// Value returns the current smoothed mapping output.
func (mapper *Mapper) Value() float64 {
	return mapper.smoother.Value()
}

func validateRange(name string, value unit.Range) error {
	if err := validateFinite(name+" minimum", value.Min); err != nil {
		return err
	}
	if err := validateFinite(name+" maximum", value.Max); err != nil {
		return err
	}
	if value.Min >= value.Max {
		return fmt.Errorf("invalid %s: minimum must be less than maximum", name)
	}
	if math.IsInf(value.Max-value.Min, 0) {
		return fmt.Errorf("invalid %s: span must be finite", name)
	}
	return nil
}

func validateFinite(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("invalid %s: value must be finite", name)
	}
	return nil
}

func clamp01(value float64) float64 {
	return min(1, max(0, value))
}
