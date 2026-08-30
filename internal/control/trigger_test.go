package control

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParseTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Trigger
	}{
		{"above:95", Trigger{Kind: TriggerAbove, Threshold: 95}},
		{"below:20", Trigger{Kind: TriggerBelow, Threshold: 20}},
		{"rise:80", Trigger{Kind: TriggerRise, Threshold: 80}},
		{"fall:50", Trigger{Kind: TriggerFall, Threshold: 50}},
		{"above:-.5", Trigger{Kind: TriggerAbove, Threshold: -0.5}},
		{"rise:2k", Trigger{Kind: TriggerRise, Threshold: 2_000}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTrigger(test.input)
			if err != nil {
				t.Fatalf("ParseTrigger(%q) returned error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ParseTrigger(%q) = %#v, want %#v", test.input, got, test.want)
			}
		})
	}
}

func TestParseTriggerRejectsMalformedExpressions(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"", "above", "above:", ":1", "above:1:2", "over:95",
		"Above:95", "rise:+1", "fall:NaN", "below: 1", "below:1 ",
	}
	for _, input := range inputs {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTrigger(input)
			if err == nil {
				t.Fatalf("ParseTrigger(%q) returned no error", input)
			}
			if !strings.Contains(err.Error(), "invalid trigger") {
				t.Fatalf("ParseTrigger(%q) error = %q, want trigger context", input, err)
			}
		})
	}
}

func TestLevelTriggerEdgeBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger Trigger
		values  []float64
		want    []bool
	}{
		{"above", Trigger{Kind: TriggerAbove, Threshold: 10}, []float64{9, 10, 11, 12, 10}, []bool{false, false, true, true, false}},
		{"below", Trigger{Kind: TriggerBelow, Threshold: 10}, []float64{11, 10, 9, 8, 10}, []bool{false, false, true, true, false}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state, err := NewTriggerState(test.trigger)
			if err != nil {
				t.Fatalf("NewTriggerState returned error: %v", err)
			}
			got := evaluateScalarSequence(t, state, test.values)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("results = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEdgeTriggerCrossingsAndEquality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger Trigger
		values  []float64
		want    []bool
	}{
		{
			"rise",
			Trigger{Kind: TriggerRise, Threshold: 10},
			[]float64{9, 10, 11, 12, 10, 11, 9, 11},
			[]bool{false, false, true, false, false, true, false, true},
		},
		{
			"fall",
			Trigger{Kind: TriggerFall, Threshold: 10},
			[]float64{11, 10, 9, 8, 10, 9, 11, 9},
			[]bool{false, false, true, false, false, true, false, true},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state, err := NewTriggerState(test.trigger)
			if err != nil {
				t.Fatalf("NewTriggerState returned error: %v", err)
			}
			got := evaluateScalarSequence(t, state, test.values)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("results = %v, want %v", got, test.want)
			}
			state.Reset()
			first, err := state.Evaluate(test.values[2])
			if err != nil {
				t.Fatalf("Evaluate after Reset returned error: %v", err)
			}
			if first {
				t.Error("edge trigger emitted on first evaluation after Reset")
			}
		})
	}
}

func TestVectorTriggerTracksIndicesIndependently(t *testing.T) {
	t.Parallel()

	state, err := NewVectorTriggerState(Trigger{Kind: TriggerRise, Threshold: 10})
	if err != nil {
		t.Fatalf("NewVectorTriggerState returned error: %v", err)
	}
	tests := []struct {
		values []float64
		want   []bool
	}{
		{[]float64{5, 15, 10}, []bool{false, false, false}},
		{[]float64{11, 14, 10}, []bool{true, false, false}},
		{[]float64{12, 9, 11}, []bool{false, false, true}},
		{[]float64{10, 11, 12}, []bool{false, true, false}},
	}
	for _, test := range tests {
		got, err := state.Evaluate(test.values)
		if err != nil {
			t.Fatalf("Evaluate(%v) returned error: %v", test.values, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("Evaluate(%v) = %v, want %v", test.values, got, test.want)
		}
	}

	state.Reset()
	first, err := state.Evaluate([]float64{11, 11})
	if err != nil {
		t.Fatalf("Evaluate after Reset returned error: %v", err)
	}
	if !reflect.DeepEqual(first, []bool{false, false}) {
		t.Fatalf("first results after Reset = %v, want no edges", first)
	}
}

func TestVectorTriggerDiscardsRemovedIndexState(t *testing.T) {
	t.Parallel()

	state, _ := NewVectorTriggerState(Trigger{Kind: TriggerRise, Threshold: 10})
	if _, err := state.Evaluate([]float64{5, 5}); err != nil {
		t.Fatalf("initial Evaluate returned error: %v", err)
	}
	if _, err := state.Evaluate([]float64{5}); err != nil {
		t.Fatalf("shrinking Evaluate returned error: %v", err)
	}
	got, err := state.Evaluate([]float64{5, 11})
	if err != nil {
		t.Fatalf("regrowing Evaluate returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []bool{false, false}) {
		t.Fatalf("regrown results = %v, want new index history to start empty", got)
	}
}

func TestTriggerStateValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewTriggerState(Trigger{Kind: TriggerKind("over"), Threshold: 1}); err == nil {
		t.Error("NewTriggerState accepted an invalid kind")
	}
	if _, err := NewVectorTriggerState(Trigger{Kind: TriggerAbove, Threshold: math.NaN()}); err == nil {
		t.Error("NewVectorTriggerState accepted a non-finite threshold")
	}
	state, _ := NewTriggerState(Trigger{Kind: TriggerRise, Threshold: 1})
	if _, err := state.Evaluate(math.Inf(1)); err == nil {
		t.Error("Evaluate accepted a non-finite value")
	}

	vector, _ := NewVectorTriggerState(Trigger{Kind: TriggerRise, Threshold: 10})
	if _, err := vector.Evaluate([]float64{5, math.NaN()}); err == nil {
		t.Error("vector Evaluate accepted a non-finite value")
	}
	got, err := vector.Evaluate([]float64{11, 11})
	if err != nil {
		t.Fatalf("valid vector Evaluate returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []bool{false, false}) {
		t.Fatalf("failed vector evaluation mutated state: results = %v", got)
	}
}

func evaluateScalarSequence(t *testing.T, state *TriggerState, values []float64) []bool {
	t.Helper()
	results := make([]bool, len(values))
	for index, value := range values {
		active, err := state.Evaluate(value)
		if err != nil {
			t.Fatalf("Evaluate(%v) returned error: %v", value, err)
		}
		results[index] = active
	}
	return results
}
