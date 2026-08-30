package sound

import (
	"fmt"
	"math"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

// Model is the complete backend-independent persistent signal/effect state.
type Model struct {
	Voices  []Voice
	Effects []Effect
}

// Validate checks every voice and effect in declaration order.
func (model Model) Validate() error {
	if len(model.Voices) == 0 {
		return fmt.Errorf("sound model requires at least one voice")
	}
	for index, voice := range model.Voices {
		if err := voice.Validate(); err != nil {
			return fmt.Errorf("voice %d: %w", index, err)
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
}

// ResolveTarget resolves a public numeric target. An effect target binds to
// the most recently declared matching effect, as required by the CLI contract.
func ResolveTarget(effects []Effect, name string) (Target, error) {
	switch name {
	case "freq", "gain", "pan", "gate":
		return Target{Name: name, EffectIndex: -1}, nil
	case "filter.cutoff", "filter.q":
		return resolveLastEffect(effects, name, func(kind EffectKind) bool {
			return kind == EffectLowPass || kind == EffectHighPass
		}, "filter")
	case "delay.time", "delay.feedback", "delay.mix":
		return resolveLastEffect(effects, name, func(kind EffectKind) bool {
			return kind == EffectDelay
		}, "delay")
	case "drive.amount":
		return resolveLastEffect(effects, name, func(kind EffectKind) bool {
			return kind == EffectDrive
		}, "drive")
	default:
		return Target{}, fmt.Errorf("unknown modulation target %q", name)
	}
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
	switch target.Name {
	case "freq":
		err = validatePositiveRange("frequency", value)
	case "gain":
		err = validateBoundedRange("gain", value, 0, 1)
	case "pan":
		err = validateBoundedRange("pan", value, -1, 1)
	case "gate":
		err = validateBoundedRange("gate", value, 0, 1)
	case "filter.cutoff":
		err = validatePositiveRange("filter cutoff", value)
	case "filter.q":
		err = validatePositiveRange("filter Q", value)
	case "delay.time":
		err = validatePositiveRange("delay time", value)
	case "delay.feedback":
		err = validateBoundedRange("delay feedback", value, 0, 0.95)
	case "delay.mix":
		err = validateBoundedRange("delay mix", value, 0, 1)
	case "drive.amount":
		err = validateBoundedRange("drive amount", value, 0, 1)
	default:
		err = fmt.Errorf("unknown modulation target %q", target.Name)
	}
	if err != nil {
		return fmt.Errorf("invalid mapping range for target %s: %w", target.Name, err)
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
	switch target.Name {
	case "filter.cutoff":
		effect.Cutoff = value
	case "filter.q":
		effect.Q = value
	case "delay.time":
		effect.DelayTime = time.Duration(value * float64(time.Second))
	case "delay.feedback":
		effect.Feedback = value
	case "delay.mix":
		effect.Mix = value
	case "drive.amount":
		effect.Amount = value
	default:
		return fmt.Errorf("target %s is not an effect target", target.Name)
	}
	return nil
}

func (target Target) validateValue(value float64) error {
	switch target.Name {
	case "freq", "filter.cutoff", "filter.q":
		return validateGreaterThanZero(target.Name, value)
	case "delay.time":
		if err := validateGreaterThanZero(target.Name, value); err != nil {
			return err
		}
		if value > float64(time.Duration(1<<63-1))/float64(time.Second) {
			return fmt.Errorf("invalid delay.time: duration is too large")
		}
		return nil
	case "gain", "gate", "delay.mix", "drive.amount":
		return validateRange(target.Name, value, 0, 1)
	case "pan":
		return validateRange(target.Name, value, -1, 1)
	case "delay.feedback":
		return validateRange(target.Name, value, 0, 0.95)
	default:
		return fmt.Errorf("unknown modulation target %q", target.Name)
	}
}

func targetMatchesEffect(name string, kind EffectKind) bool {
	switch name {
	case "filter.cutoff", "filter.q":
		return kind == EffectLowPass || kind == EffectHighPass
	case "delay.time", "delay.feedback", "delay.mix":
		return kind == EffectDelay
	case "drive.amount":
		return kind == EffectDrive
	default:
		return false
	}
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
