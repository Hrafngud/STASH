package control

import (
	"fmt"
	"strings"

	"github.com/zalmo/stash/internal/unit"
)

// TriggerKind identifies threshold-level or threshold-edge behavior.
type TriggerKind string

const (
	TriggerAbove TriggerKind = "above"
	TriggerBelow TriggerKind = "below"
	TriggerRise  TriggerKind = "rise"
	TriggerFall  TriggerKind = "fall"
)

// Trigger is a parsed trigger definition.
type Trigger struct {
	Kind      TriggerKind
	Threshold float64
}

// ParseTrigger parses above:VALUE, below:VALUE, rise:VALUE, or fall:VALUE.
func ParseTrigger(input string) (Trigger, error) {
	if strings.Count(input, ":") != 1 {
		return Trigger{}, fmt.Errorf("invalid trigger %q: expected KIND:VALUE", input)
	}
	kindPart, thresholdPart, _ := strings.Cut(input, ":")
	kind := TriggerKind(kindPart)
	switch kind {
	case TriggerAbove, TriggerBelow, TriggerRise, TriggerFall:
	default:
		return Trigger{}, fmt.Errorf("invalid trigger %q: unknown kind %q", input, kindPart)
	}
	if thresholdPart == "" {
		return Trigger{}, fmt.Errorf("invalid trigger %q: missing threshold", input)
	}
	threshold, err := unit.ParseNumber(thresholdPart)
	if err != nil {
		return Trigger{}, fmt.Errorf("invalid trigger %q: threshold: %w", input, err)
	}
	return Trigger{Kind: kind, Threshold: threshold}, nil
}

// TriggerState evaluates one scalar stream and retains the previous value
// needed by edge triggers.
type TriggerState struct {
	trigger  Trigger
	previous float64
	hasPrev  bool
}

// NewTriggerState creates independent evaluation state for a trigger.
func NewTriggerState(trigger Trigger) (*TriggerState, error) {
	if err := validateTrigger(trigger); err != nil {
		return nil, err
	}
	return &TriggerState{trigger: trigger}, nil
}

// Evaluate reports whether the trigger is active or emitted for value. Edge
// triggers never emit on the first evaluation because no crossing is known.
func (state *TriggerState) Evaluate(value float64) (bool, error) {
	if err := validateFinite("trigger value", value); err != nil {
		return false, err
	}

	active := false
	switch state.trigger.Kind {
	case TriggerAbove:
		active = value > state.trigger.Threshold
	case TriggerBelow:
		active = value < state.trigger.Threshold
	case TriggerRise:
		active = state.hasPrev && state.previous <= state.trigger.Threshold && value > state.trigger.Threshold
	case TriggerFall:
		active = state.hasPrev && state.previous >= state.trigger.Threshold && value < state.trigger.Threshold
	}
	state.previous = value
	state.hasPrev = true
	return active, nil
}

// Reset clears crossing history.
func (state *TriggerState) Reset() {
	state.previous = 0
	state.hasPrev = false
}

// VectorTriggerState evaluates each vector index with independent crossing
// history.
type VectorTriggerState struct {
	trigger Trigger
	states  []*TriggerState
}

// NewVectorTriggerState creates an empty per-index trigger evaluator.
func NewVectorTriggerState(trigger Trigger) (*VectorTriggerState, error) {
	if err := validateTrigger(trigger); err != nil {
		return nil, err
	}
	return &VectorTriggerState{trigger: trigger}, nil
}

// Evaluate reports trigger results in vector index order. If the vector
// shrinks, removed index history is discarded.
func (state *VectorTriggerState) Evaluate(values []float64) ([]bool, error) {
	for index, value := range values {
		if err := validateFinite("trigger value", value); err != nil {
			return nil, fmt.Errorf("evaluate trigger at vector index %d: %w", index, err)
		}
	}
	if len(values) < len(state.states) {
		state.states = state.states[:len(values)]
	}
	for len(state.states) < len(values) {
		indexState, err := NewTriggerState(state.trigger)
		if err != nil {
			return nil, err
		}
		state.states = append(state.states, indexState)
	}

	results := make([]bool, len(values))
	for index, value := range values {
		active, err := state.states[index].Evaluate(value)
		if err != nil {
			return nil, fmt.Errorf("evaluate trigger at vector index %d: %w", index, err)
		}
		results[index] = active
	}
	return results, nil
}

// Reset clears all per-index crossing history.
func (state *VectorTriggerState) Reset() {
	state.states = nil
}

func validateTrigger(trigger Trigger) error {
	switch trigger.Kind {
	case TriggerAbove, TriggerBelow, TriggerRise, TriggerFall:
	default:
		return fmt.Errorf("invalid trigger kind %q", trigger.Kind)
	}
	if err := validateFinite("trigger threshold", trigger.Threshold); err != nil {
		return err
	}
	return nil
}
