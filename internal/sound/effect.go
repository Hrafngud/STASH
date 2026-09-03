package sound

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

type EffectKind string

const (
	EffectLowPass        EffectKind = "low-pass"
	EffectHighPass       EffectKind = "high-pass"
	EffectBandPass       EffectKind = "band-pass"
	EffectNotch          EffectKind = "notch"
	EffectPeak           EffectKind = "peak"
	EffectLowShelf       EffectKind = "low-shelf"
	EffectHighShelf      EffectKind = "high-shelf"
	EffectDelay          EffectKind = "delay"
	EffectDrive          EffectKind = "drive"
	EffectChorus         EffectKind = "chorus"
	EffectFlanger        EffectKind = "flanger"
	EffectPhaser         EffectKind = "phaser"
	EffectReverb         EffectKind = "reverb"
	EffectTremolo        EffectKind = "tremolo"
	EffectPan            EffectKind = "pan"
	EffectWidth          EffectKind = "width"
	EffectHaas           EffectKind = "haas"
	EffectCrush          EffectKind = "crush"
	EffectShape          EffectKind = "shape"
	EffectComb           EffectKind = "comb"
	EffectAllpass        EffectKind = "allpass"
	EffectCompressor     EffectKind = "comp"
	EffectLimiter        EffectKind = "limiter"
	EffectGate           EffectKind = "gate"
	EffectResonator      EffectKind = "reson"
	EffectRing           EffectKind = "ring"
	EffectFrequencyShift EffectKind = "freqshift"
	EffectFold           EffectKind = "fold"
	EffectFormant        EffectKind = "formant"
	EffectPitch          EffectKind = "pitch"
	EffectStutter        EffectKind = "stutter"
	EffectGranular       EffectKind = "grain"
	EffectFreeze         EffectKind = "freeze"
	EffectSpectralBlur   EffectKind = "spectral.blur"
	EffectSpectralShift  EffectKind = "spectral.shift"
	EffectConvolution    EffectKind = "conv"
)

const DefaultFilterQ = 0.707

type EffectParameter struct {
	Name           string
	Default        float64
	Minimum        *float64
	Maximum        *float64
	StrictPositive bool
	Unit           string
	Integer        bool
}

type EffectSpec struct {
	Kind       EffectKind
	Name       string
	Target     string
	Parameters []EffectParameter
	Positional []string
}

func bound(value float64) *float64 { return &value }
func param(name string, value float64, minimum, maximum *float64, positive bool, unit string) EffectParameter {
	return EffectParameter{Name: name, Default: value, Minimum: minimum, Maximum: maximum, StrictPositive: positive, Unit: unit}
}

var effectSpecs = []EffectSpec{
	{EffectLowPass, "lp", "filter", []EffectParameter{param("cutoff", 1000, bound(0), nil, true, "hz"), param("q", DefaultFilterQ, bound(0), nil, true, "")}, []string{"cutoff", "q"}},
	{EffectHighPass, "hp", "filter", []EffectParameter{param("cutoff", 1000, bound(0), nil, true, "hz"), param("q", DefaultFilterQ, bound(0), nil, true, "")}, []string{"cutoff", "q"}},
	{EffectBandPass, "bp", "filter", []EffectParameter{param("cutoff", 1000, bound(0), nil, true, "hz"), param("q", DefaultFilterQ, bound(0), nil, true, "")}, []string{"cutoff", "q"}},
	{EffectNotch, "notch", "filter", []EffectParameter{param("cutoff", 1000, bound(0), nil, true, "hz"), param("q", DefaultFilterQ, bound(0), nil, true, "")}, []string{"cutoff", "q"}},
	{EffectPeak, "peak", "filter", []EffectParameter{param("cutoff", 2000, bound(0), nil, true, "hz"), param("q", 6, bound(0), nil, true, ""), param("gain", 9, bound(-60), bound(60), false, "db")}, []string{"cutoff", "q", "gain"}},
	{EffectLowShelf, "shelf.low", "filter", []EffectParameter{param("cutoff", 200, bound(0), nil, true, "hz"), param("gain", 6, bound(-60), bound(60), false, "db")}, []string{"cutoff", "gain"}},
	{EffectHighShelf, "shelf.high", "filter", []EffectParameter{param("cutoff", 8000, bound(0), nil, true, "hz"), param("gain", -4, bound(-60), bound(60), false, "db")}, []string{"cutoff", "gain"}},
	{EffectDelay, "delay", "delay", []EffectParameter{param("time", .15, bound(0), nil, true, "s"), param("feedback", .4, bound(0), bound(.95), false, ""), param("mix", .25, bound(0), bound(1), false, "")}, []string{"time", "feedback", "mix"}},
	{EffectDrive, "drive", "drive", []EffectParameter{param("amount", .5, bound(0), bound(1), false, "")}, []string{"amount"}},
	{EffectChorus, "chorus", "chorus", []EffectParameter{param("rate", .8, bound(0), bound(20), true, "hz"), param("depth", .3, bound(0), bound(1), false, ""), param("mix", .25, bound(0), bound(1), false, "")}, []string{"rate", "depth", "mix"}},
	{EffectFlanger, "flanger", "flanger", []EffectParameter{param("rate", .2, bound(0), bound(20), true, "hz"), param("depth", .005, bound(0), bound(.05), true, "s"), param("feedback", .4, bound(-.95), bound(.95), false, ""), param("mix", .5, bound(0), bound(1), false, "")}, []string{"rate", "depth", "feedback", "mix"}},
	{EffectPhaser, "phaser", "phaser", []EffectParameter{param("rate", .3, bound(0), bound(20), true, "hz"), param("depth", .7, bound(0), bound(1), false, ""), param("feedback", .3, bound(-.95), bound(.95), false, ""), {Name: "stages", Default: 6, Minimum: bound(1), Maximum: bound(16), Integer: true}}, []string{"rate", "depth", "stages", "feedback"}},
	{EffectReverb, "reverb", "reverb", []EffectParameter{param("size", .7, bound(0), bound(1), false, ""), param("damp", .4, bound(0), bound(1), false, ""), param("mix", .25, bound(0), bound(1), false, "")}, []string{"size", "damp", "mix"}},
	{EffectTremolo, "tremolo", "tremolo", []EffectParameter{param("rate", 6, bound(0), bound(40), true, "hz"), param("depth", .5, bound(0), bound(1), false, "")}, []string{"rate", "depth"}},
	{EffectPan, "pan", "pan", []EffectParameter{param("position", 0, bound(-1), bound(1), false, ""), param("rate", 0, bound(0), bound(20), false, "hz"), param("depth", 1, bound(0), bound(1), false, "")}, []string{"position"}},
	{EffectWidth, "width", "width", []EffectParameter{param("amount", 1, bound(0), bound(2), false, "")}, []string{"amount"}},
	{EffectHaas, "haas", "haas", []EffectParameter{param("delay", .012, bound(0), bound(.05), true, "s")}, []string{"delay"}},
	{EffectCrush, "crush", "crush", []EffectParameter{param("bits", 8, bound(1), bound(24), false, ""), param("rate", 12000, bound(100), bound(48000), false, "hz")}, []string{"bits", "rate"}},
	{EffectShape, "shape", "shape", []EffectParameter{param("drive", .5, bound(0), bound(1), false, ""), param("bias", 0, bound(-1), bound(1), false, "")}, []string{"drive", "bias"}},
	{EffectComb, "comb", "comb", []EffectParameter{param("delay", .012, bound(0), bound(1), true, "s"), param("feedback", .6, bound(-.95), bound(.95), false, "")}, []string{"delay", "feedback"}},
	{EffectAllpass, "allpass", "allpass", []EffectParameter{param("delay", .012, bound(0), bound(1), true, "s"), param("feedback", .6, bound(-.95), bound(.95), false, "")}, []string{"delay", "feedback"}},
	{EffectCompressor, "comp", "comp", []EffectParameter{param("threshold", -12, bound(-80), bound(0), false, "db"), param("ratio", 4, bound(1), bound(100), false, ""), param("attack", .005, bound(0), bound(10), true, "s"), param("release", .08, bound(0), bound(10), true, "s")}, []string{"threshold", "ratio", "attack", "release"}},
	{EffectLimiter, "limiter", "limiter", []EffectParameter{param("threshold", -1, bound(-40), bound(0), false, "db"), param("release", .05, bound(0), bound(10), true, "s")}, []string{"threshold", "release"}},
	{EffectGate, "gate", "gate", []EffectParameter{param("threshold", -35, bound(-100), bound(0), false, "db"), param("attack", .005, bound(0), bound(10), true, "s"), param("release", .08, bound(0), bound(10), true, "s")}, []string{"threshold", "attack", "release"}},
	{EffectResonator, "reson", "reson", []EffectParameter{param("freq", 440, bound(0), nil, true, "hz"), param("q", 12, bound(0), nil, true, "")}, []string{"freq", "q"}},
	{EffectRing, "ring", "ring", []EffectParameter{param("freq", 80, bound(0), bound(24000), true, "hz"), param("mix", .5, bound(0), bound(1), false, "")}, []string{"freq", "mix"}},
	{EffectFrequencyShift, "freqshift", "freqshift", []EffectParameter{param("amount", 30, bound(-24000), bound(24000), false, "hz"), param("mix", 1, bound(0), bound(1), false, "")}, []string{"amount", "mix"}},
	{EffectFold, "fold", "fold", []EffectParameter{param("amount", .4, bound(0), bound(4), false, "")}, []string{"amount"}},
	{EffectFormant, "formant", "formant", []EffectParameter{param("position", 0, bound(0), bound(1), false, "")}, []string{"position"}},
	{EffectPitch, "pitch", "pitch", []EffectParameter{param("semitones", 0, bound(-48), bound(48), false, "st"), param("mix", 1, bound(0), bound(1), false, "")}, []string{"semitones", "mix"}},
	{EffectStutter, "stutter", "stutter", []EffectParameter{param("size", .08, bound(0), bound(2), true, "s"), {Name: "repeats", Default: 4, Minimum: bound(1), Maximum: bound(64), Integer: true}, param("prob", 1, bound(0), bound(1), false, "")}, []string{"size", "repeats", "prob"}},
	{EffectGranular, "grain", "grain", []EffectParameter{param("size", .08, bound(0), bound(1), true, "s"), param("density", 12, bound(.1), bound(200), false, "hz"), param("jitter", .2, bound(0), bound(1), false, ""), param("pitch", 1, bound(.125), bound(8), false, ""), param("mix", 1, bound(0), bound(1), false, "")}, []string{"size", "density", "jitter", "pitch", "mix"}},
	{EffectFreeze, "freeze", "freeze", []EffectParameter{param("amount", .8, bound(0), bound(1), false, "")}, []string{"amount"}},
	{EffectSpectralBlur, "spectral.blur", "spectral.blur", []EffectParameter{param("amount", .4, bound(0), bound(2), false, ""), param("mix", 1, bound(0), bound(1), false, "")}, []string{"amount", "mix"}},
	{EffectSpectralShift, "spectral.shift", "spectral.shift", []EffectParameter{param("amount", 120, bound(-24000), bound(24000), false, "hz"), param("mix", 1, bound(0), bound(1), false, "")}, []string{"amount", "mix"}},
	{EffectConvolution, "conv", "conv", []EffectParameter{param("mix", .4, bound(0), bound(1), false, "")}, []string{"mix"}},
}

var specsByKind = func() map[EffectKind]EffectSpec {
	result := map[EffectKind]EffectSpec{}
	for _, spec := range effectSpecs {
		result[spec.Kind] = spec
	}
	return result
}()

type Effect struct {
	Kind       EffectKind
	Parameters map[string]float64
	Config     map[string]string
	// Kept for compatibility with the original public model.
	Cutoff    float64
	Q         float64
	DelayTime time.Duration
	Feedback  float64
	Mix       float64
	Amount    float64
}

func LookupEffectSpec(kind EffectKind) (EffectSpec, bool) {
	spec, ok := specsByKind[kind]
	return spec, ok
}

// EffectSpecs returns the public effect registry in discovery order. The
// returned slice and its nested parameter/positional slices are independent
// copies so interactive consumers cannot mutate parser metadata.
func EffectSpecs() []EffectSpec {
	result := make([]EffectSpec, len(effectSpecs))
	for index, spec := range effectSpecs {
		result[index] = spec
		result[index].Parameters = append([]EffectParameter(nil), spec.Parameters...)
		result[index].Positional = append([]string(nil), spec.Positional...)
	}
	return result
}

func EffectParameterNames(effect Effect) []string {
	spec, ok := LookupEffectSpec(effect.Kind)
	if !ok {
		return nil
	}
	names := make([]string, len(spec.Parameters))
	for index, parameter := range spec.Parameters {
		names[index] = parameter.Name
	}
	return names
}

func (effect Effect) Parameter(name string) (float64, bool) {
	if value, ok := effect.Parameters[name]; ok {
		return value, true
	}
	switch name {
	case "cutoff":
		if effect.Kind == EffectLowPass || effect.Kind == EffectHighPass {
			return effect.Cutoff, true
		}
	case "q":
		if effect.Kind == EffectLowPass || effect.Kind == EffectHighPass {
			return effect.Q, true
		}
	case "time":
		if effect.Kind == EffectDelay {
			return effect.DelayTime.Seconds(), true
		}
	case "feedback":
		if effect.Kind == EffectDelay {
			return effect.Feedback, true
		}
	case "mix":
		if effect.Kind == EffectDelay {
			return effect.Mix, true
		}
	case "amount":
		if effect.Kind == EffectDrive {
			return effect.Amount, true
		}
	}
	return 0, false
}

func (effect *Effect) SetParameter(name string, value float64) error {
	spec, ok := LookupEffectSpec(effect.Kind)
	if !ok {
		return fmt.Errorf("unknown effect kind %q", effect.Kind)
	}
	parameter, ok := specParameter(spec, name)
	if !ok {
		return fmt.Errorf("effect %s has no parameter %s", spec.Name, name)
	}
	if parameter.Integer {
		value = math.Round(value)
	}
	if err := validateEffectParameter(parameter, value); err != nil {
		return err
	}
	if effect.Parameters == nil {
		effect.Parameters = map[string]float64{}
	}
	effect.Parameters[name] = value
	switch name {
	case "cutoff":
		effect.Cutoff = value
	case "q":
		effect.Q = value
	case "time":
		effect.DelayTime = time.Duration(value * float64(time.Second))
	case "feedback":
		effect.Feedback = value
	case "mix":
		effect.Mix = value
	case "amount":
		effect.Amount = value
	}
	return nil
}

func (effect Effect) Validate() error {
	spec, ok := LookupEffectSpec(effect.Kind)
	if !ok {
		return fmt.Errorf("unknown effect kind %q", effect.Kind)
	}
	for _, parameter := range spec.Parameters {
		value, exists := effect.Parameter(parameter.Name)
		if !exists {
			value = parameter.Default
		}
		if err := validateEffectParameter(parameter, value); err != nil {
			return fmt.Errorf("%s %s: %w", spec.Name, parameter.Name, err)
		}
	}
	if effect.Kind == EffectConvolution && strings.TrimSpace(effect.Config["impulse"]) == "" {
		return fmt.Errorf("conv impulse must not be empty")
	}
	return nil
}

func ParseFilter(input string) (Effect, error) {
	name, arguments, found := strings.Cut(input, ":")
	if !found || name == "" || arguments == "" || strings.Contains(arguments, ":") {
		return Effect{}, fmt.Errorf("invalid filter %q: expected TYPE:CUTOFF[,Q[,GAIN]]", input)
	}
	spec, ok := specByName(name)
	if !ok || spec.Target != "filter" {
		return Effect{}, fmt.Errorf("unknown filter %q: expected lp, hp, bp, notch, peak, shelf.low, or shelf.high", name)
	}
	effect, err := parseEffectSpec(spec, arguments)
	if err != nil {
		return Effect{}, fmt.Errorf("invalid filter %q: %w", input, err)
	}
	return effect, nil
}

func ParseEffect(input string) (Effect, error) {
	name, arguments, found := strings.Cut(input, ":")
	if !found || name == "" || arguments == "" {
		return Effect{}, fmt.Errorf("invalid effect %q: expected NAME:ARGUMENTS", input)
	}
	original := name
	switch name {
	case "autopan":
		name = "pan"
	case "bitcrush", "downsample":
		name = "crush"
	case "waveshaper":
		name = "shape"
	case "spectral.freeze":
		name = "freeze"
	}
	spec, ok := specByName(name)
	if !ok || spec.Target == "filter" {
		return Effect{}, fmt.Errorf("unknown effect %q", original)
	}
	if spec.Kind == EffectConvolution {
		return parseConvolution(spec, arguments, input)
	}
	if original == "autopan" && !strings.Contains(arguments, "=") {
		parts := strings.Split(arguments, ",")
		if len(parts) == 2 {
			arguments = "rate=" + parts[0] + ",depth=" + parts[1]
		}
	}
	if original == "downsample" && !strings.Contains(arguments, "=") {
		arguments = "rate=" + arguments
	}
	effect, err := parseEffectSpec(spec, arguments)
	if err != nil {
		if spec.Kind == EffectDelay {
			return Effect{}, fmt.Errorf("invalid delay %q: expected delay:TIME,FEEDBACK,MIX: %w", input, err)
		}
		if spec.Kind == EffectDrive {
			return Effect{}, fmt.Errorf("invalid drive %q: expected drive:AMOUNT: %w", input, err)
		}
		return Effect{}, fmt.Errorf("invalid %s %q: %w", original, input, err)
	}
	if original == "autopan" && effect.Parameters["rate"] == 0 {
		effect.Parameters["rate"] = .3
	}
	if original == "bitcrush" {
		effect.Parameters["rate"] = 48000
	}
	if original == "downsample" {
		effect.Parameters["bits"] = 24
	}
	return effect, nil
}

func parseEffectSpec(spec EffectSpec, arguments string) (Effect, error) {
	effect := Effect{Kind: spec.Kind, Parameters: map[string]float64{}, Config: map[string]string{}}
	for _, parameter := range spec.Parameters {
		effect.Parameters[parameter.Name] = parameter.Default
	}
	parts := strings.Split(arguments, ",")
	named := len(parts) > 0 && strings.Contains(parts[0], "=")
	if !named && spec.Kind == EffectDelay && len(parts) != len(spec.Positional) {
		return Effect{}, fmt.Errorf("expected three positional arguments")
	}
	for index, part := range parts {
		if part == "" {
			return Effect{}, fmt.Errorf("empty argument")
		}
		name, token := "", part
		if named {
			var found bool
			name, token, found = strings.Cut(part, "=")
			if !found || name == "" || token == "" {
				return Effect{}, fmt.Errorf("expected NAME=VALUE")
			}
		} else {
			if strings.Contains(part, "=") || index >= len(spec.Positional) {
				return Effect{}, fmt.Errorf("too many or mixed positional arguments")
			}
			name = spec.Positional[index]
		}
		if name == "curve" {
			if spec.Kind != EffectShape || (token != "tanh" && token != "clip" && token != "atan") {
				return Effect{}, fmt.Errorf("curve must be tanh, clip, or atan")
			}
			effect.Config[name] = token
			continue
		}
		if name == "vowel" {
			positions := map[string]float64{"a": 0, "e": .25, "i": .5, "o": .75, "u": 1}
			value, ok := positions[strings.ToLower(token)]
			if !ok || spec.Kind != EffectFormant {
				return Effect{}, fmt.Errorf("vowel must be a, e, i, o, or u")
			}
			effect.Parameters["position"] = value
			continue
		}
		if name == "ratio" && spec.Kind == EffectPitch {
			ratio, err := unit.ParseNumber(token)
			if err != nil || ratio <= 0 {
				return Effect{}, fmt.Errorf("ratio must be greater than zero")
			}
			effect.Parameters["semitones"] = 12 * math.Log2(ratio)
			continue
		}
		parameter, ok := specParameter(spec, name)
		if !ok {
			return Effect{}, fmt.Errorf("unknown parameter %q", name)
		}
		value, err := parseEffectValue(parameter, token)
		label := spec.Name + " " + name
		if spec.Target == "filter" {
			label = "filter " + name
		}
		if err != nil {
			return Effect{}, fmt.Errorf("%s: %w", label, err)
		}
		if err := validateEffectParameter(parameter, value); err != nil {
			return Effect{}, fmt.Errorf("%s: %w", label, err)
		}
		effect.Parameters[name] = value
	}
	if spec.Kind == EffectShape && effect.Config["curve"] == "" {
		effect.Config["curve"] = "tanh"
	}
	for name, value := range effect.Parameters {
		_ = effect.SetParameter(name, value)
	}
	return effect, nil
}

func parseConvolution(spec EffectSpec, arguments, input string) (Effect, error) {
	parts := strings.Split(arguments, ",")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		return Effect{}, fmt.Errorf("invalid conv %q: expected conv:IMPULSE[,MIX]", input)
	}
	effect := Effect{Kind: spec.Kind, Parameters: map[string]float64{"mix": .4}, Config: map[string]string{"impulse": parts[0]}}
	if len(parts) == 2 {
		value, err := parseEffectValue(spec.Parameters[0], parts[1])
		if err != nil {
			return Effect{}, err
		}
		if err := effect.SetParameter("mix", value); err != nil {
			return Effect{}, err
		}
	}
	return effect, nil
}

func specByName(name string) (EffectSpec, bool) {
	for _, spec := range effectSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return EffectSpec{}, false
}

func specParameter(spec EffectSpec, name string) (EffectParameter, bool) {
	for _, parameter := range spec.Parameters {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return EffectParameter{}, false
}

func parseEffectValue(parameter EffectParameter, token string) (float64, error) {
	if parameter.Unit == "s" {
		duration, err := unit.ParseDuration(token)
		if err != nil {
			return 0, err
		}
		return duration.Seconds(), nil
	}
	if parameter.Unit == "db" && strings.HasSuffix(strings.ToLower(token), "db") {
		token = token[:len(token)-2]
	}
	if parameter.Unit == "st" {
		if strings.HasSuffix(strings.ToLower(token), "st") {
			token = token[:len(token)-2]
		}
		if strings.HasPrefix(token, "+") {
			token = token[1:]
		}
	}
	return unit.ParseNumber(token)
}

func validateEffectParameter(parameter EffectParameter, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("must be finite")
	}
	if parameter.StrictPositive && value <= 0 {
		return fmt.Errorf("must be greater than zero")
	}
	if parameter.Minimum != nil && value < *parameter.Minimum || parameter.Maximum != nil && value > *parameter.Maximum {
		return fmt.Errorf("must be between %s and %s", formatBound(parameter.Minimum), formatBound(parameter.Maximum))
	}
	if parameter.Integer && math.Trunc(value) != value {
		return fmt.Errorf("must be an integer")
	}
	return nil
}

func formatBound(value *float64) string {
	if value == nil {
		return "infinity"
	}
	return strconv.FormatFloat(*value, 'g', -1, 64)
}
