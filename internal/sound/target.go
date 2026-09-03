package sound

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

// Model is the complete backend-independent persistent signal/effect state.
type Model struct {
	Voices        []Voice
	Synths        []Synth
	AudioRoutes   []AudioRoute
	Effects       []Effect
	MasterGain    float64
	MasterGainSet bool
}

// Validate checks every voice and effect in declaration order.
func (model Model) Validate() error {
	if len(model.Voices) == 0 && len(model.Synths) == 0 {
		return fmt.Errorf("sound model requires at least one voice or synth")
	}
	for index, voice := range model.Voices {
		if err := voice.Validate(); err != nil {
			return fmt.Errorf("voice %d: %w", index, err)
		}
	}
	ids := map[string]bool{}
	for index, synth := range model.Synths {
		if ids[synth.ID] {
			return fmt.Errorf("duplicate synth id: %s", synth.ID)
		}
		ids[synth.ID] = true
		if err := synth.Validate(); err != nil {
			return fmt.Errorf("synth %d: %w", index, err)
		}
	}
	if model.MasterGainSet {
		if err := validateRange("master gain", model.MasterGain, 0, 1); err != nil {
			return err
		}
	}
	for index, route := range model.AudioRoutes {
		if route.SourceIndex < 0 || route.SourceIndex >= len(model.Synths) {
			return fmt.Errorf("audio route %d source index out of range", index)
		}
		if !route.Target.IsSynth || route.Target.SynthIndex < 0 || route.Target.SynthIndex >= len(model.Synths) {
			return fmt.Errorf("audio route %d target is not a valid synth target", index)
		}
	}
	for index, effect := range model.Effects {
		if err := effect.Validate(); err != nil {
			return fmt.Errorf("effect %d: %w", index, err)
		}
	}
	return nil
}

// Target binds a public modulation target to either voice parameters or one
// concrete effect-chain element. EffectIndex is -1 for voice targets.
type Target struct {
	Name        string
	EffectIndex int
	IsSynth     bool
	SynthIndex  int
	Mod         bool
	Vector      bool
}

// Value reads the current numeric value for a bound target. Delay time is
// exposed in seconds, matching Set and the public modulation grammar.
func (target Target) Value(model Model, voiceIndex int) (float64, error) {
	if target.IsSynth {
		if target.SynthIndex < 0 || target.SynthIndex >= len(model.Synths) {
			return 0, fmt.Errorf("synth index %d out of range", target.SynthIndex)
		}
		name := target.Name
		if target.Vector {
			name = fmt.Sprintf("partial.%d.gain", voiceIndex)
		}
		value, ok := model.Synths[target.SynthIndex].Parameters[name]
		if !ok {
			return 0, fmt.Errorf("synth %s has no parameter: %s", model.Synths[target.SynthIndex].ID, name)
		}
		if target.Mod {
			return model.Synths[target.SynthIndex].Modulations[target.Name], nil
		}
		return value, nil
	}
	if target.EffectIndex < 0 {
		if voiceIndex < 0 || voiceIndex >= len(model.Voices) {
			return 0, fmt.Errorf("voice index %d out of range", voiceIndex)
		}
		voice := model.Voices[voiceIndex]
		switch target.Name {
		case "freq":
			return voice.Frequency, nil
		case "gain":
			return voice.Gain, nil
		case "pan":
			return voice.Pan, nil
		case "gate":
			return voice.Gate, nil
		default:
			return 0, fmt.Errorf("target %s is not a voice target", target.Name)
		}
	}
	if target.EffectIndex >= len(model.Effects) {
		return 0, fmt.Errorf("effect index %d out of range", target.EffectIndex)
	}
	effect := model.Effects[target.EffectIndex]
	if !targetMatchesEffect(target.Name, effect.Kind) {
		return 0, fmt.Errorf("target %s does not match effect %d kind %s", target.Name, target.EffectIndex, effect.Kind)
	}
	parameter, ok := effectParameterName(effect.Kind, target.Name)
	if !ok {
		return 0, fmt.Errorf("target %s is not an effect target", target.Name)
	}
	value, ok := effect.Parameter(parameter)
	if !ok {
		return 0, fmt.Errorf("effect %d has no parameter %s", target.EffectIndex, parameter)
	}
	return value, nil
}

// ResolveModelTarget resolves synth, legacy voice, and effect targets. An
// unqualified synth target binds to the most recently declared synth.
func ResolveModelTarget(model Model, name string) (Target, error) {
	if name == "freq" || name == "gain" || name == "pan" || name == "gate" {
		if len(model.Synths) > 0 {
			return resolveSynthParameter(model, len(model.Synths)-1, name, false)
		}
		return Target{Name: name, EffectIndex: -1}, nil
	}
	if strings.HasPrefix(name, "syn.") {
		parts := strings.Split(strings.TrimPrefix(name, "syn."), ".")
		mod := len(parts) > 0 && parts[len(parts)-1] == "mod"
		if mod {
			parts = parts[:len(parts)-1]
		}
		if len(parts) == 0 {
			return Target{}, fmt.Errorf("invalid modulation target %q", name)
		}
		index := len(model.Synths) - 1
		parameterParts := parts
		if len(parts) >= 2 {
			if found := synthIndexByID(model.Synths, parts[0]); found >= 0 {
				index, parameterParts = found, parts[1:]
			}
		}
		if index < 0 || len(parameterParts) == 0 {
			return Target{}, fmt.Errorf("modulation target %q requires a declared synth", name)
		}
		return resolveSynthParameter(model, index, strings.Join(parameterParts, "."), mod)
	}
	return ResolveTarget(model.Effects, name)
}

func synthIndexByID(synths []Synth, id string) int {
	for index := len(synths) - 1; index >= 0; index-- {
		if synths[index].ID == id {
			return index
		}
	}
	return -1
}

func resolveSynthParameter(model Model, index int, parameter string, mod bool) (Target, error) {
	if index < 0 || index >= len(model.Synths) {
		return Target{}, fmt.Errorf("synth index %d out of range", index)
	}
	synth := model.Synths[index]
	if parameter == "partials.gain" && synth.Type == SynthAdd && !mod {
		return Target{Name: parameter, EffectIndex: -1, IsSynth: true, SynthIndex: index, Vector: true}, nil
	}
	if _, ok := synth.Parameters[parameter]; !ok {
		return Target{}, fmt.Errorf("synth %s has no parameter: %s", synth.ID, parameter)
	}
	return Target{Name: parameter, EffectIndex: -1, IsSynth: true, SynthIndex: index, Mod: mod}, nil
}

func (target Target) SupportsAudioRate(model Model) bool {
	if !target.IsSynth || target.SynthIndex < 0 || target.SynthIndex >= len(model.Synths) {
		return false
	}
	parameter, ok := model.Synths[target.SynthIndex].Spec().Parameters[target.Name]
	return ok && parameter.AudioRate
}

// ResolveTarget resolves a public numeric target. An effect target binds to
// the most recently declared matching effect, as required by the CLI contract.
func ResolveTarget(effects []Effect, name string) (Target, error) {
	switch name {
	case "freq", "gain", "pan", "gate":
		return Target{Name: name, EffectIndex: -1}, nil
	}
	known, required := false, ""
	for _, spec := range effectSpecs {
		if _, ok := effectParameterName(spec.Kind, name); ok {
			known, required = true, spec.Target
			break
		}
	}
	if !known {
		return Target{}, fmt.Errorf("unknown modulation target %q", name)
	}
	return resolveLastEffect(effects, name, func(kind EffectKind) bool { return targetMatchesEffect(name, kind) }, required)
}

func resolveLastEffect(effects []Effect, name string, matches func(EffectKind) bool, required string) (Target, error) {
	for index := len(effects) - 1; index >= 0; index-- {
		if matches(effects[index].Kind) {
			return Target{Name: name, EffectIndex: index}, nil
		}
	}
	return Target{}, fmt.Errorf("modulation target %q requires a declared %s effect", name, required)
}

// ValidateRange ensures every value a mapping can produce is valid for the
// bound target.
func (target Target) ValidateRange(value unit.Range) error {
	if math.IsNaN(value.Min) || math.IsInf(value.Min, 0) || math.IsNaN(value.Max) || math.IsInf(value.Max, 0) {
		return fmt.Errorf("target %s range must be finite", target.Name)
	}
	if value.Min >= value.Max {
		return fmt.Errorf("target %s range minimum must be less than maximum", target.Name)
	}
	var err error
	if target.IsSynth {
		if target.Mod {
			return nil
		}
		return fmt.Errorf("synth target range requires model context")
	}
	switch target.Name {
	case "freq":
		err = validatePositiveRange("frequency", value)
	case "gain":
		err = validateBoundedRange("gain", value, 0, 1)
	case "pan":
		err = validateBoundedRange("pan", value, -1, 1)
	case "gate":
		err = validateBoundedRange("gate", value, 0, 1)
	default:
		parameter, ok := parameterForTarget(target.Name)
		if !ok {
			err = fmt.Errorf("unknown modulation target %q", target.Name)
			break
		}
		if parameter.StrictPositive && value.Min <= 0 {
			err = fmt.Errorf("%s must be greater than zero", target.Name)
			break
		}
		if parameter.Minimum != nil && value.Min < *parameter.Minimum || parameter.Maximum != nil && value.Max > *parameter.Maximum {
			err = fmt.Errorf("%s must be between %v and %v", target.Name, boundText(parameter.Minimum), boundText(parameter.Maximum))
		}
	}
	if err != nil {
		return fmt.Errorf("invalid mapping range for target %s: %w", target.Name, err)
	}
	return nil
}

// ValidateModelRange checks a target range using the concrete synth spec when
// needed and delegates legacy/effect targets to ValidateRange.
func (target Target) ValidateModelRange(model Model, value unit.Range) error {
	if !target.IsSynth {
		return target.ValidateRange(value)
	}
	if math.IsNaN(value.Min) || math.IsInf(value.Min, 0) || math.IsNaN(value.Max) || math.IsInf(value.Max, 0) || value.Min >= value.Max {
		return fmt.Errorf("invalid mapping range for target %s: range must be finite and increasing", target.Name)
	}
	if target.Mod {
		return nil
	}
	if target.Vector {
		if value.Min < 0 || value.Max > 1 {
			return fmt.Errorf("invalid mapping range for target %s: partial gain must be between 0 and 1", target.Name)
		}
		return nil
	}
	if target.SynthIndex < 0 || target.SynthIndex >= len(model.Synths) {
		return fmt.Errorf("synth index %d out of range", target.SynthIndex)
	}
	parameter, ok := model.Synths[target.SynthIndex].Spec().Parameters[target.Name]
	if !ok {
		if strings.HasSuffix(target.Name, ".gain") {
			if err := validateBoundedRange("partial gain", value, 0, 1); err != nil {
				return fmt.Errorf("invalid mapping range for target %s: %w", target.Name, err)
			}
			return nil
		}
		if strings.HasSuffix(target.Name, ".ratio") {
			if err := validatePositiveRange("partial ratio", value); err != nil {
				return fmt.Errorf("invalid mapping range for target %s: %w", target.Name, err)
			}
			return nil
		}
		if strings.HasSuffix(target.Name, ".detune") {
			return nil
		}
		return fmt.Errorf("synth %s has no parameter: %s", model.Synths[target.SynthIndex].ID, target.Name)
	}
	if parameter.Minimum != nil {
		if (*parameter.Minimum == 0 && isStrictPositive(target.Name) && value.Min <= 0) || value.Min < *parameter.Minimum {
			return fmt.Errorf("invalid mapping range for target %s: minimum is out of range", target.Name)
		}
	}
	if parameter.Maximum != nil && value.Max > *parameter.Maximum {
		return fmt.Errorf("invalid mapping range for target %s: maximum is out of range", target.Name)
	}
	return nil
}

// Set writes a numeric target. Voice targets require a valid voice index;
// effect targets use the effect index fixed during resolution. Delay time is
// represented to controls in seconds and stored as a duration.
func (target Target) Set(model *Model, voiceIndex int, value float64) error {
	if model == nil {
		return fmt.Errorf("sound model is nil")
	}
	if target.IsSynth {
		if target.SynthIndex < 0 || target.SynthIndex >= len(model.Synths) {
			return fmt.Errorf("synth index %d out of range", target.SynthIndex)
		}
		if target.Mod {
			if err := validateFinite(target.Name+" modulation", value); err != nil {
				return err
			}
			if model.Synths[target.SynthIndex].Modulations == nil {
				model.Synths[target.SynthIndex].Modulations = map[string]float64{}
			}
			model.Synths[target.SynthIndex].Modulations[target.Name] = value
			return nil
		}
		if target.Vector {
			if value < 0 || value > 1 {
				return fmt.Errorf("invalid partial gain: out of range")
			}
			name := fmt.Sprintf("partial.%d.gain", voiceIndex)
			if _, ok := model.Synths[target.SynthIndex].Parameters[name]; !ok {
				return fmt.Errorf("partial index %d out of range", voiceIndex)
			}
			model.Synths[target.SynthIndex].Parameters[name] = value
			return nil
		}
		parameter, ok := model.Synths[target.SynthIndex].Spec().Parameters[target.Name]
		if !ok {
			if strings.HasSuffix(target.Name, ".gain") && (value < 0 || value > 1) {
				return fmt.Errorf("invalid %s: out of range", target.Name)
			}
			if strings.HasSuffix(target.Name, ".ratio") && value <= 0 {
				return fmt.Errorf("invalid %s: must be positive", target.Name)
			}
			model.Synths[target.SynthIndex].Parameters[target.Name] = value
			return nil
		}
		if parameter.Minimum != nil && ((*parameter.Minimum == 0 && isStrictPositive(target.Name) && value <= 0) || value < *parameter.Minimum) {
			return fmt.Errorf("invalid %s: below minimum", target.Name)
		}
		if parameter.Maximum != nil && value > *parameter.Maximum {
			return fmt.Errorf("invalid %s: above maximum", target.Name)
		}
		model.Synths[target.SynthIndex].Parameters[target.Name] = value
		return nil
	}
	if err := target.validateValue(value); err != nil {
		return err
	}
	if target.EffectIndex < 0 {
		if voiceIndex < 0 || voiceIndex >= len(model.Voices) {
			return fmt.Errorf("voice index %d out of range", voiceIndex)
		}
		voice := &model.Voices[voiceIndex]
		switch target.Name {
		case "freq":
			voice.Frequency = value
		case "gain":
			voice.Gain = value
		case "pan":
			voice.Pan = value
		case "gate":
			voice.Gate = value
		default:
			return fmt.Errorf("target %s is not a voice target", target.Name)
		}
		return nil
	}
	if target.EffectIndex >= len(model.Effects) {
		return fmt.Errorf("effect index %d out of range", target.EffectIndex)
	}
	effect := &model.Effects[target.EffectIndex]
	if !targetMatchesEffect(target.Name, effect.Kind) {
		return fmt.Errorf("target %s does not match effect %d kind %s", target.Name, target.EffectIndex, effect.Kind)
	}
	parameter, ok := effectParameterName(effect.Kind, target.Name)
	if !ok {
		return fmt.Errorf("target %s is not an effect target", target.Name)
	}
	return effect.SetParameter(parameter, value)
}

func (target Target) validateValue(value float64) error {
	switch target.Name {
	case "freq":
		return validateGreaterThanZero(target.Name, value)
	case "gain", "gate":
		return validateRange(target.Name, value, 0, 1)
	case "pan":
		return validateRange(target.Name, value, -1, 1)
	default:
		parameter, ok := parameterForTarget(target.Name)
		if !ok {
			return fmt.Errorf("unknown modulation target %q", target.Name)
		}
		if parameter.Unit == "s" && value > float64(time.Duration(1<<63-1))/float64(time.Second) {
			return fmt.Errorf("invalid %s: duration is too large", target.Name)
		}
		if err := validateEffectParameter(parameter, value); err != nil {
			return fmt.Errorf("invalid %s: %w", target.Name, err)
		}
		return nil
	}
}

func targetMatchesEffect(name string, kind EffectKind) bool {
	_, ok := effectParameterName(kind, name)
	return ok
}

func effectParameterName(kind EffectKind, target string) (string, bool) {
	spec, ok := LookupEffectSpec(kind)
	if !ok {
		return "", false
	}
	for _, parameter := range spec.Parameters {
		if target == spec.Target+"."+parameter.Name {
			return parameter.Name, true
		}
	}
	return "", false
}

func parameterForTarget(target string) (EffectParameter, bool) {
	for _, spec := range effectSpecs {
		if parameter, ok := effectParameterName(spec.Kind, target); ok {
			return specParameter(spec, parameter)
		}
	}
	return EffectParameter{}, false
}

func boundText(value *float64) any {
	if value == nil {
		return "infinity"
	}
	return *value
}

func validatePositiveRange(name string, value unit.Range) error {
	if value.Min <= 0 {
		return fmt.Errorf("%s must be greater than zero", name)
	}
	if name == "delay time" && value.Max > float64(time.Duration(1<<63-1))/float64(time.Second) {
		return fmt.Errorf("%s duration is too large", name)
	}
	return nil
}

func validateBoundedRange(name string, value unit.Range, minimum, maximum float64) error {
	if value.Min < minimum || value.Max > maximum {
		return fmt.Errorf("%s must be between %v and %v", name, minimum, maximum)
	}
	return nil
}
