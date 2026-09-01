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
  stash -i SOURCE
  stash -p PRIMITIVE
  stash -h | --help

Sources:
  SOURCE                         Canonical source name, such as cpu.usage
  -                              Read one numeric sample per non-empty stdin line

Discovery:
  -l [PREFIX]                    List sources, optionally filtered by prefix
  -i SOURCE                      Inspect a source and its availability
  -p PRIMITIVE                   Resolve a note, note list, scale, mode, or rhythm
  -h, --help                     Show this help and exit

Sound and mapping:
  -w WAVE                        Waveform: sine, square, saw, tri, or noise
  -m [CONTROL:]TARGET=MAP        Add a modulation mapping (repeatable)
  --range [CONTROL=]MIN..MAX     Override an input range (repeatable)
  -v GAIN                        Static output gain, from 0 to 1

Notes and rhythm:
  -t TRIGGER                     above:X, below:X, rise:X, or fall:X
  -n NOTES                       Note, note list, scale, or mode material
  -r RHYTHM                      rhythm:[BPM:]DIVISION:PATTERN
  -b BPM                         Set or override rhythm tempo
  -d TIME                        Event gate duration, such as 150ms
  -a A,D,S,R                     ADSR envelope, such as 5ms,40ms,.7,100ms
  --swing AMOUNT                 Swing percentage, from 50 to 75

Effects and output:
  -f FILTER                      lp:CUTOFF[,Q] or hp:CUTOFF[,Q] (repeatable)
  -x EFFECT                      delay:TIME,FEEDBACK,MIX or drive:AMOUNT (repeatable)
  -o -                           Write raw 48 kHz stereo float32 PCM to stdout

MAP is MIN..MAX[/linear|exp|log][~TIME]. Effects keep declaration order.
With no sound option, SOURCE writes telemetry samples to stdout.

Examples:
  stash -l cpu
  stash -i cpu.usage
  stash cpu.usage -w sine -m freq=80..2k/exp~150ms
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
