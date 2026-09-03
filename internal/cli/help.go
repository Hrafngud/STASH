package cli

import (
	"fmt"
	"io"
)

// HelpText is the concise command reference printed by -h and --help.
// Detailed grammar and semantics belong in the stash(1) manual page.
const HelpText = `STASH — Sound Telemetry Auto SHell

Turn live Linux hardware and operating-system telemetry into sound.

Usage:
  stash SOURCE [OPTIONS]
  stash -l [PREFIX]
  stash -i NAME
  stash -p PRIMITIVE
  stash -h | --help

Sources:
  SOURCE                         Canonical source name, such as cpu.usage
  -                              Read one numeric sample per non-empty stdin line

Discovery:
  -l [PREFIX]                    List sources or synths (use "syn")
  -i NAME                        Inspect a source or synth (such as syn.fm)
  -p PRIMITIVE                   Resolve note material, a rhythm, or a synth
  -h, --help                     Show this help and exit

Sound and mapping:
  -s TYPE[:ID][,PARAM=VALUE]...  Add a synth graph node (repeatable)
  -w WAVE                        Set the legacy/current synth waveform
  -m [CONTROL:]TARGET=MAP        Add a modulation mapping (repeatable)
  --range [CONTROL=]MIN..MAX     Override an input range (repeatable)
  -v GAIN                        Set output/master gain, from 0 to 1

Notes and rhythm:
  -t TRIGGER                     above:X, below:X, rise:X, or fall:X
  -n NOTES                       Note, note list, scale, or mode material
  -r RHYTHM                      rhythm:[BPM:]DIVISION:PATTERN
  -b BPM                         Set or override rhythm tempo
  -d TIME                        Event gate duration, such as 150ms
  -a A,D,S,R                     ADSR envelope, such as 5ms,40ms,.7,100ms
  --swing AMOUNT                 Swing percentage, from 50 to 75

Effects and output:
  -f FILTER                      lp, hp, bp, notch, peak, or shelf filter (repeatable)
  -x EFFECT                      Add an ordered DSP effect; named args supported (repeatable)
  -o -                           Write raw 48 kHz stereo float32 PCM to stdout

Synth types: sub, fm, pm, am, ring, add, wavetable, karplus, modal, granular.
Targets are syn.PARAM or syn.ID.PARAM; additive inputs end in .mod.
Each synth exposes an audio-rate control named syn.ID.out.
MAP is MIN..MAX[/linear|exp|log][~TIME]. Every numeric effect parameter is a target.
With no sound option, SOURCE writes telemetry samples to stdout.

Examples:
  stash -l cpu
  stash -i cpu.usage
  stash cpu.usage -w sine -m freq=80..2k/exp~150ms
  stash cpu.usage -s fm:mod,mix=0 -s sub:voice -m freq=40..120 \
    -m syn.mod.out:syn.voice.freq.mod=-200..200
  stash -p syn.fm:bass,ratio=2,index=4
  stash -p mode:E3:phrygian:12

See stash(1) for the complete reference.
`

// IsHelpRequest reports whether args is exactly one public help flag. Help is
// intentionally a top-level command so invalid option combinations remain
// visible to the normal parser.
func IsHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "-h" || args[0] == "--help")
}

// WriteHelp writes the stable help text to output.
func WriteHelp(output io.Writer) error {
	if output == nil {
		return fmt.Errorf("write help: stdout is nil")
	}
	if _, err := io.WriteString(output, HelpText); err != nil {
		return fmt.Errorf("write help: %w", err)
	}
	return nil
}
