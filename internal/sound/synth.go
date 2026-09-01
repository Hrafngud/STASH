package sound

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

// SynthType identifies one canonical built-in synthesis graph.
type SynthType string

const (
	SynthSub       SynthType = "sub"
	SynthFM        SynthType = "fm"
	SynthPM        SynthType = "pm"
	SynthAM        SynthType = "am"
	SynthRing      SynthType = "ring"
	SynthAdd       SynthType = "add"
	SynthWavetable SynthType = "wavetable"
	SynthKarplus   SynthType = "karplus"
	SynthModal     SynthType = "modal"
	SynthGranular  SynthType = "granular"
)

// ParameterSpec describes one public numeric synth inlet.
type ParameterSpec struct {
	Name        string
	Unit        string
	Default     float64
	Minimum     *float64
	Maximum     *float64
	AudioRate   bool
	Description string
}

// ConfigSpec describes one graph-time synth setting.
type ConfigSpec struct {
	Name        string
	Required    bool
	Default     string
	Description string
}

// SynthSpec is the discovery and validation definition of a built-in synth.
type SynthSpec struct {
	Type        SynthType
	Description string
	Parameters  map[string]ParameterSpec
	Config      map[string]ConfigSpec
	Waveform    bool
}

// Synth is one resolved synthesis graph node. Numeric values are runtime
// bases; Config values are fixed while the graph is running.
type Synth struct {
	Type        SynthType
	ID          string
	ExplicitID  bool
	Parameters  map[string]float64
	Modulations map[string]float64
	Config      map[string]string
	Explicit    map[string]bool
	Envelope    ADSR
}

// AudioRoute is a compiled synth-output modulation edge. SourceIndex points
// to a pre-mix, pre-pan bipolar output and Target points to a synth parameter.
type AudioRoute struct {
	SourceIndex int
	Target      Target
	OutputMin   float64
	OutputMax   float64
	Curve       string
	Smoothing   time.Duration
}

func ptr(value float64) *float64 { return &value }

func commonParameters() map[string]ParameterSpec {
	return map[string]ParameterSpec{
		"freq": {Name: "freq", Unit: "Hz", Default: 440, Minimum: ptr(0), AudioRate: true, Description: "fundamental frequency"},
		"gain": {Name: "gain", Unit: "linear", Default: .1, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true, Description: "node amplitude"},
		"pan":  {Name: "pan", Unit: "linear", Default: 0, Minimum: ptr(-1), Maximum: ptr(1), Description: "stereo position"},
		"gate": {Name: "gate", Unit: "linear", Default: 1, Minimum: ptr(0), Maximum: ptr(1), Description: "envelope gate"},
		"mix":  {Name: "mix", Unit: "linear", Default: 1, Minimum: ptr(0), Maximum: ptr(1), Description: "master-mix contribution"},
	}
}

func synthParams(extra ...ParameterSpec) map[string]ParameterSpec {
	parameters := commonParameters()
	for _, parameter := range extra {
		parameters[parameter.Name] = parameter
	}
	return parameters
}

var synthSpecs = map[SynthType]SynthSpec{
	SynthSub: {
		Type: SynthSub, Description: "oscillator, filter, and VCA subtractive voice", Waveform: true,
		Config: map[string]ConfigSpec{"wave": {Name: "wave", Default: "sine"}, "filter": {Name: "filter", Default: "lp"}},
		Parameters: synthParams(
			ParameterSpec{Name: "cutoff", Unit: "Hz", Default: 20_000, Minimum: ptr(0)},
			ParameterSpec{Name: "q", Unit: "ratio", Default: .707, Minimum: ptr(0)},
			ParameterSpec{Name: "pulsewidth", Unit: "linear", Default: .5, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
		),
	},
	SynthFM: modulationSpec(SynthFM, "two-oscillator frequency modulation voice"),
	SynthPM: modulationSpec(SynthPM, "two-oscillator phase modulation voice"),
	SynthAM: {
		Type: SynthAM, Description: "carrier with unipolar amplitude modulation", Waveform: true,
		Config: waveConfig(), Parameters: synthParams(
			ParameterSpec{Name: "ratio", Unit: "ratio", Default: 1, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "modfreq", Unit: "Hz", Default: 440, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "depth", Unit: "linear", Default: 1, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
		),
	},
	SynthRing: {
		Type: SynthRing, Description: "bipolar carrier/modulator multiplication", Waveform: true,
		Config: waveConfig(), Parameters: synthParams(
			ParameterSpec{Name: "ratio", Unit: "ratio", Default: 1, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "modfreq", Unit: "Hz", Default: 440, Minimum: ptr(0), AudioRate: true},
		),
	},
	SynthAdd: {
		Type: SynthAdd, Description: "oscillator-bank additive voice", Waveform: true,
		Config:     map[string]ConfigSpec{"wave": {Name: "wave", Default: "sine"}, "partials": {Name: "partials", Default: "8"}},
		Parameters: synthParams(),
	},
	SynthWavetable: {
		Type: SynthWavetable, Description: "interpolating wavetable oscillator",
		Config: map[string]ConfigSpec{"table": {Name: "table", Required: true}},
		Parameters: synthParams(
			ParameterSpec{Name: "position", Unit: "linear", Default: 0, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
			ParameterSpec{Name: "scan", Unit: "Hz", Default: 0, Minimum: ptr(0), AudioRate: true},
		),
	},
	SynthKarplus: {
		Type: SynthKarplus, Description: "excitation, damped delay, and bounded feedback string",
		Parameters: synthParams(
			ParameterSpec{Name: "excite", Unit: "linear", Default: 1, Minimum: ptr(0), Maximum: ptr(1)},
			ParameterSpec{Name: "damping", Unit: "linear", Default: .5, Minimum: ptr(0), Maximum: ptr(1)},
			ParameterSpec{Name: "feedback", Unit: "linear", Default: .98, Minimum: ptr(0), Maximum: ptr(.999)},
			ParameterSpec{Name: "brightness", Unit: "linear", Default: .5, Minimum: ptr(0), Maximum: ptr(1)},
		),
	},
	SynthModal: {
		Type: SynthModal, Description: "excitation and resonator-bank physical model",
		Config: map[string]ConfigSpec{"model": {Name: "model", Required: true}},
		Parameters: synthParams(
			ParameterSpec{Name: "excite", Unit: "linear", Default: 1, Minimum: ptr(0), Maximum: ptr(1)},
			ParameterSpec{Name: "decay", Unit: "seconds", Default: 1, Minimum: ptr(0)},
			ParameterSpec{Name: "brightness", Unit: "linear", Default: .5, Minimum: ptr(0), Maximum: ptr(1)},
			ParameterSpec{Name: "inharmonicity", Unit: "linear", Default: 0, Minimum: ptr(0)},
		),
	},
	SynthGranular: {
		Type: SynthGranular, Description: "sample buffer, grain scheduler, and grain-voice mixer",
		Config: map[string]ConfigSpec{"sample": {Name: "sample", Required: true}},
		Parameters: synthParams(
			ParameterSpec{Name: "density", Unit: "grains/s", Default: 10, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "size", Unit: "s", Default: .1, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "position", Unit: "linear", Default: 0, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
			ParameterSpec{Name: "pitch", Unit: "ratio", Default: 1, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "jitter", Unit: "linear", Default: 0, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
			ParameterSpec{Name: "spread", Unit: "linear", Default: 0, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
		),
	},
}

func modulationSpec(kind SynthType, description string) SynthSpec {
	return SynthSpec{
		Type: kind, Description: description, Waveform: true, Config: waveConfig(),
		Parameters: synthParams(
			ParameterSpec{Name: "ratio", Unit: "ratio", Default: 1, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "modfreq", Unit: "Hz", Default: 440, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "index", Unit: "linear", Default: 1, Minimum: ptr(0), AudioRate: true},
			ParameterSpec{Name: "feedback", Unit: "linear", Default: 0, Minimum: ptr(0), Maximum: ptr(1), AudioRate: true},
		),
	}
}

func waveConfig() map[string]ConfigSpec {
	return map[string]ConfigSpec{
		"wave": {Name: "wave", Default: "sine"}, "modwave": {Name: "modwave", Default: "sine"},
	}
}

// SynthTypes returns canonical synth types in discovery order.
func SynthTypes() []SynthType {
	return []SynthType{SynthSub, SynthFM, SynthPM, SynthAM, SynthRing, SynthAdd, SynthWavetable, SynthKarplus, SynthModal, SynthGranular}
}

func LookupSynthSpec(kind SynthType) (SynthSpec, bool) { spec, ok := synthSpecs[kind]; return spec, ok }

var synthIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// ParseSynth parses TYPE[:ID][,PARAM=VALUE...] and validates local settings.
func ParseSynth(input string) (Synth, error) {
	parts := strings.Split(input, ",")
	if parts[0] == "" {
		return Synth{}, fmt.Errorf("invalid synth declaration %q", input)
	}
	typeToken, id, explicit := parts[0], "", false
	if before, after, found := strings.Cut(typeToken, ":"); found {
		if strings.Contains(after, ":") || before == "" || after == "" || !synthIDPattern.MatchString(after) {
			return Synth{}, fmt.Errorf("invalid synth id %q: must start with a letter and contain only letters, numbers, _ or -", after)
		}
		typeToken, id, explicit = before, after, true
	}
	kind := SynthType(typeToken)
	spec, ok := LookupSynthSpec(kind)
	if !ok {
		return Synth{}, fmt.Errorf("unknown synth: %s", typeToken)
	}
	synth := Synth{Type: kind, ID: id, ExplicitID: explicit, Parameters: map[string]float64{}, Modulations: map[string]float64{}, Config: map[string]string{}, Explicit: map[string]bool{}, Envelope: DefaultADSR}
	for name, parameter := range spec.Parameters {
		synth.Parameters[name] = parameter.Default
	}
	for name, config := range spec.Config {
		if config.Default != "" {
			synth.Config[name] = config.Default
		}
	}
	if kind == SynthAdd {
		for _, assignment := range parts[1:] {
			name, value, found := strings.Cut(assignment, "=")
			if found && name == "partials" {
				if err := validateConfigValue(kind, name, value); err != nil {
					return Synth{}, err
				}
				synth.Config[name] = value
			}
		}
		count, _ := strconv.Atoi(synth.Config["partials"])
		for index := 0; index < count; index++ {
			synth.Parameters[fmt.Sprintf("partial.%d.gain", index)] = 1 / float64(index+1)
			synth.Parameters[fmt.Sprintf("partial.%d.ratio", index)] = float64(index + 1)
			synth.Parameters[fmt.Sprintf("partial.%d.detune", index)] = 0
		}
	}
	seen := map[string]bool{}
	for _, assignment := range parts[1:] {
		if strings.Count(assignment, "=") != 1 {
			return Synth{}, fmt.Errorf("invalid synth parameter %q: expected PARAM=VALUE", assignment)
		}
		name, value, _ := strings.Cut(assignment, "=")
		if name == "" || value == "" || seen[name] {
			return Synth{}, fmt.Errorf("invalid synth parameter %q", assignment)
		}
		seen[name] = true
		synth.Explicit[name] = true
		if parameter, exists := spec.Parameters[name]; exists {
			parsed, err := parseSynthNumber(parameter, value)
			if err != nil {
				return Synth{}, fmt.Errorf("invalid %s synth parameter %s=%s: %w", kind, name, value, err)
			}
			synth.Parameters[name] = parsed
			continue
		}
		if kind == SynthAdd && isPartialParameter(name, synth) {
			parsed, err := unit.ParseNumber(value)
			if err != nil {
				return Synth{}, fmt.Errorf("invalid additive parameter %s: %w", name, err)
			}
			if strings.HasSuffix(name, ".gain") && (parsed < 0 || parsed > 1) {
				return Synth{}, fmt.Errorf("invalid additive parameter %s: gain must be between 0 and 1", name)
			}
			if strings.HasSuffix(name, ".ratio") && parsed <= 0 {
				return Synth{}, fmt.Errorf("invalid additive parameter %s: ratio must be greater than zero", name)
			}
			synth.Parameters[name] = parsed
			continue
		}
		if _, exists := spec.Config[name]; exists {
			if err := validateConfigValue(kind, name, value); err != nil {
				return Synth{}, err
			}
			synth.Config[name] = value
			continue
		}
		return Synth{}, fmt.Errorf("synth %s has no parameter: %s", kind, name)
	}
	for name, config := range spec.Config {
		if config.Required && synth.Config[name] == "" {
			return Synth{}, fmt.Errorf("%s synth requires %s", kind, name)
		}
	}
	if (kind == SynthFM || kind == SynthPM || kind == SynthAM || kind == SynthRing) && seen["ratio"] && seen["modfreq"] {
		return Synth{}, fmt.Errorf("%s synth %s cannot define both ratio and modfreq", kind, displaySynthID(synth))
	}
	return synth, nil
}

func isPartialParameter(name string, synth Synth) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 3 || parts[0] != "partial" {
		return false
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	count, _ := strconv.Atoi(synth.Config["partials"])
	if index < 0 || index >= count {
		return false
	}
	return parts[2] == "gain" || parts[2] == "ratio" || parts[2] == "detune"
}

func parseSynthNumber(spec ParameterSpec, input string) (float64, error) {
	var value float64
	var err error
	if spec.Unit == "s" {
		var duration time.Duration
		duration, err = unit.ParseDuration(input)
		value = duration.Seconds()
	} else {
		value, err = unit.ParseNumber(input)
	}
	if err != nil {
		return 0, err
	}
	if spec.Minimum != nil && ((*spec.Minimum == 0 && value <= 0 && isStrictPositive(spec.Name)) || value < *spec.Minimum) {
		if *spec.Minimum == 0 && isStrictPositive(spec.Name) {
			return 0, fmt.Errorf("must be greater than zero")
		}
		return 0, fmt.Errorf("must be at least %g", *spec.Minimum)
	}
	if spec.Maximum != nil && value > *spec.Maximum {
		return 0, fmt.Errorf("must be at most %g", *spec.Maximum)
	}
	return value, nil
}

func isStrictPositive(name string) bool {
	switch name {
	case "freq", "cutoff", "q", "ratio", "modfreq", "density", "size", "pitch", "decay":
		return true
	}
	return false
}

func validateConfigValue(kind SynthType, name, value string) error {
	switch name {
	case "wave", "modwave":
		if _, err := ParseWaveform(value); err != nil {
			return err
		}
	case "filter":
		if value != "lp" && value != "hp" {
			return fmt.Errorf("invalid sub filter %q: expected lp or hp", value)
		}
	case "partials":
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 || count > 128 {
			return fmt.Errorf("invalid additive partials %q: expected integer from 1 to 128", value)
		}
	case "model":
		switch value {
		case "metal", "wood", "glass", "bell", "plate":
		default:
			return fmt.Errorf("invalid modal model %q", value)
		}
	case "table":
		if value == "" {
			return fmt.Errorf("wavetable synth requires table")
		}
	case "sample":
		if value == "" {
			return fmt.Errorf("granular synth requires sample")
		}
	}
	return nil
}

// ParseWaveform validates a public oscillator waveform.
func ParseWaveform(value string) (Waveform, error) {
	wave := Waveform(value)
	switch wave {
	case WaveSine, WaveSquare, WaveSaw, WaveTri, WaveNoise:
		return wave, nil
	}
	return "", fmt.Errorf("unknown waveform %q: expected sine, square, saw, tri, or noise", value)
}

func displaySynthID(synth Synth) string {
	if synth.ID != "" {
		return synth.ID
	}
	return string(synth.Type)
}

// AssignSynthIDs fills omitted IDs deterministically and rejects duplicates.
func AssignSynthIDs(synths []Synth) error {
	used := map[string]bool{}
	for index := range synths {
		id := synths[index].ID
		if id == "" {
			base := string(synths[index].Type)
			id = base
			for suffix := 2; used[id]; suffix++ {
				id = base + strconv.Itoa(suffix)
			}
			synths[index].ID = id
		}
		if used[id] {
			return fmt.Errorf("duplicate synth id: %s", id)
		}
		used[id] = true
	}
	return nil
}

func (synth Synth) Spec() SynthSpec { return synthSpecs[synth.Type] }

func (synth Synth) Validate() error {
	spec, ok := LookupSynthSpec(synth.Type)
	if !ok {
		return fmt.Errorf("unknown synth: %s", synth.Type)
	}
	if !synthIDPattern.MatchString(synth.ID) {
		return fmt.Errorf("invalid synth id: %s", synth.ID)
	}
	for name, parameter := range spec.Parameters {
		value, exists := synth.Parameters[name]
		if !exists {
			return fmt.Errorf("synth %s missing parameter: %s", synth.ID, name)
		}
		if err := validateFinite(name, value); err != nil {
			return fmt.Errorf("synth %s parameter %s: %w", synth.ID, name, err)
		}
		if parameter.Minimum != nil && ((*parameter.Minimum == 0 && value <= 0 && isStrictPositive(name)) || value < *parameter.Minimum) {
			return fmt.Errorf("synth %s parameter %s is below its minimum", synth.ID, name)
		}
		if parameter.Maximum != nil && value > *parameter.Maximum {
			return fmt.Errorf("synth %s parameter %s is above its maximum", synth.ID, name)
		}
	}
	for name, config := range spec.Config {
		if config.Required && synth.Config[name] == "" {
			return fmt.Errorf("%s synth requires %s", synth.Type, name)
		}
	}
	return nil
}

// SetWaveform applies -w to a synth that exposes a primary oscillator.
func (synth *Synth) SetWaveform(wave Waveform) error {
	if synth == nil {
		return fmt.Errorf("synth is nil")
	}
	if !synth.Spec().Waveform {
		return fmt.Errorf("synth %s does not support a primary waveform", synth.ID)
	}
	synth.Config["wave"] = string(wave)
	return nil
}

func SortedParameterNames(spec SynthSpec) []string {
	names := make([]string, 0, len(spec.Parameters))
	for name := range spec.Parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func SortedSynthParameterNames(synth Synth) []string {
	names := make([]string, 0, len(synth.Parameters))
	for name := range synth.Parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
