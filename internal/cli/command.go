// Package cli parses the public STASH command line and turns it into a
// validated execution plan. It does not print diagnostics or execute work.
package cli

import (
	"time"

	"github.com/zalmo/stash/internal/control"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
	"github.com/zalmo/stash/internal/unit"
)

// CommandKind identifies the mutually exclusive public command forms.
type CommandKind uint8

const (
	CommandSource CommandKind = iota
	CommandList
	CommandInspect
	CommandPrimitive
)

// Waveform is one of the waveforms exposed by -w.
type Waveform = sound.Waveform

const (
	WaveSine   = sound.WaveSine
	WaveSquare = sound.WaveSquare
	WaveSaw    = sound.WaveSaw
	WaveTri    = sound.WaveTri
	WaveNoise  = sound.WaveNoise
)

// ADSR is a parsed amplitude envelope.
type ADSR = sound.ADSR

// Modulation is one parsed [CONTROL:]TARGET=MAP declaration. An empty Control
// means the command's primary source.
type Modulation struct {
	Control string
	Target  string
	Mapping control.Mapping
}

// RangeOverride is one parsed [CONTROL=]MIN..MAX declaration. An empty
// Control means the command's primary source.
type RangeOverride struct {
	Control string
	Range   unit.Range
}

// OrderedOptionKind identifies one of the repeatable graph/chain options. Ordered
// entries retain their complete argv order, including interleaved -f and -x.
type OrderedOptionKind string

const (
	OrderedModulation OrderedOptionKind = "modulation"
	OrderedSynth      OrderedOptionKind = "synth"
	OrderedFilter     OrderedOptionKind = "filter"
	OrderedEffect     OrderedOptionKind = "effect"
)

// OrderedOption records the original argument of a repeatable option.
type OrderedOption struct {
	Kind     OrderedOptionKind
	Argument string
}

// Command is the syntax-validated representation of argv. Semantic checks
// involving source availability and relationships between options belong to
// BuildPlan.
type Command struct {
	Kind CommandKind

	Source        string
	ListPrefix    string
	InspectSource string
	Primitive     string

	Waveform       *Waveform
	Synths         []sound.Synth
	Modulations    []Modulation
	RangeOverrides []RangeOverride
	Gain           *float64
	Trigger        *control.Trigger
	Notes          []primitive.Note
	Rhythm         *primitive.Rhythm
	BPM            *float64
	GateDuration   *time.Duration
	Envelope       *ADSR
	Swing          *float64
	Output         *string
	Effects        []sound.Effect

	// Ordered retains repeatable option argv order for diagnostics and later
	// runtime routing. Effects contains the same -f/-x declarations as one
	// typed chain, without synth or modulation entries.
	Ordered []OrderedOption
}

// Mode is the deterministic execution route selected by the planner.
type Mode string

const (
	ModeList        Mode = "list"
	ModeInspect     Mode = "inspect"
	ModePrimitive   Mode = "primitive"
	ModeTelemetry   Mode = "telemetry"
	ModeAudioDevice Mode = "audio-device"
	ModeRawPCM      Mode = "raw-pcm"
)

const (
	DefaultGain         = 0.1
	DefaultGateDuration = 100 * time.Millisecond
)

var DefaultADSR = sound.DefaultADSR

// PrimitiveResolution contains the typed result of -p.
type PrimitiveResolution struct {
	Notes  []primitive.Note
	Rhythm *primitive.Rhythm
	BPM    *float64
	Synth  *sound.Synth
}

// Plan is a semantically validated command with documented audio defaults
// applied. SourceEntry is set for source execution and inspection modes.
type Plan struct {
	Mode    Mode
	Command Command

	SourceEntry source.Entry
	HasSource   bool
	Primitive   PrimitiveResolution
	SynthSpec   *sound.SynthSpec

	Waveform     Waveform
	Gain         float64
	GateDuration time.Duration
	Envelope     ADSR
	Swing        float64
	BPM          *float64

	SoundTargets []sound.Target
	Sound        sound.Model
}
