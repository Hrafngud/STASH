# SYNTAX.md

## 1. Command form

```text
stash
stash SOURCE [OPTIONS]
stash DISCOVERY
stash -h
stash --help
```

With no arguments, `stash` opens the live instrument editor described in
section 41. Every argument-bearing form retains the behavior below.

`SOURCE` is either:

```text
a canonical STASH source name
-
```

`-` means stdin.

Examples:

```bash
stash cpu.usage
stash cpu.cores.usage
stash net.enp4s0.rx
producer | stash -
```

## 2. Shell-safety contract

Normal STASH syntax does not require shell quoting.

Canonical DSL arguments do not use:

```text
>
<
*
(
)
spaces
```

Canonical DSL arguments use:

```text
.
:
,
=
..
/
~
-
```

A user may quote arguments voluntarily, but quoting is not required for valid canonical syntax.

## 3. Top-level options

```text
-h, --help                   show command help and exit
-l [PREFIX]                  list sources or synths
-i NAME                      inspect a source or synth
-p PRIMITIVE                 resolve/inspect a primitive or synth declaration

-s TYPE[:ID][,PARAM=VALUE]... add a synth node
-w WAVE                      select waveform
-m [CONTROL:]TARGET=MAP      add modulation
--range [CONTROL=]RANGE      override an input range
-v GAIN                      set static output gain

-t TRIGGER                   define telemetry trigger
-n NOTES                     define note material
-r RHYTHM                    define rhythm
-b BPM                       define/override tempo
-d TIME                      define gate duration
-a ADSR                      define amplitude envelope
--swing AMOUNT               set swing percentage

-f FILTER                    append filter
-x EFFECT                    append effect

-o TARGET                    select audio output
```

Repeated `-s`, `-m`, `--range`, `-f`, and `-x` options are allowed. `-w` may
also be repeated when it follows synth declarations.

Repeated effects preserve command-line order.

`-h` and `--help` are complete top-level command forms. They write help to
stdout and exit successfully. Combining a help flag with another argument is
an error.

## 4. Operating modes

### 4.1 Telemetry mode

A source with no audio-producing options:

```bash
stash cpu.usage
```

writes samples to stdout.

The following options activate audio mode:

```text
-s
-w
-m
-v
-t
-n
-r
-b
-d
-a
--swing
-f
-x
-o
```

`-o -` activates raw PCM mode.

### 4.2 Audio device mode

Example:

```bash
stash cpu.usage -m freq=80..2k
```

Audio goes to the default output device.

stdout is empty.

### 4.3 Raw PCM mode

Example:

```bash
stash cpu.usage -m freq=80..2k -o -
```

stdout is raw PCM.

Diagnostics remain on stderr.

## 5. Synth graph

Synths are declared with:

```text
-s TYPE[:ID][,PARAM=VALUE]...
```

Canonical types are:

```text
sub fm pm am ring add wavetable karplus modal granular
```

IDs start with a letter, contain only letters, numbers, `_` or `-`, and are
unique within a command. Omitted IDs are assigned deterministically from the
synth type: `sub`, `sub2`, and so on. Repeated synth outputs are summed before
the existing `-f`/`-x` chain.

Declaration assignments set either fixed graph configuration or a numeric base
value. Configuration and type-specific parameters are:

| Type | Configuration | Type-specific numeric parameters |
| --- | --- | --- |
| `sub` | `wave` (default `sine`), `filter=lp\|hp` (default `lp`) | `cutoff`, `q`, `pulsewidth` |
| `fm`, `pm` | `wave`, `modwave` (default `sine`) | `ratio`, `modfreq`, `index`, `feedback` |
| `am` | `wave`, `modwave` (default `sine`) | `ratio`, `modfreq`, `depth` |
| `ring` | `wave`, `modwave` (default `sine`) | `ratio`, `modfreq` |
| `add` | `wave` (default `sine`), `partials` (default `8`, range 1..128) | `partial.N.gain`, `partial.N.ratio`, `partial.N.detune` |
| `wavetable` | required `table` | `position`, `scan` |
| `karplus` | none | `excite`, `damping`, `feedback`, `brightness` |
| `modal` | required `model=metal\|wood\|glass\|bell\|plate` | `excite`, `decay`, `brightness`, `inharmonicity` |
| `granular` | required `sample` | `density`, `size`, `position`, `pitch`, `jitter`, `spread` |

`N` is a zero-based additive partial index smaller than `partials`. Wavetable
names `metal`, `digital`, and `smooth` select built-ins; other `table` values
and granular `sample` values are passed to Csound as file paths. Use
`stash -i syn.TYPE` for every parameter's unit, default, range, and audio-rate
capability.

Every synth exposes `freq`, `gain`, `pan`, `gate`, and `mix`. Type-specific
numeric parameters are also modulatable. Static values belong to the synth
declaration:

```bash
stash cpu.usage -s fm:bass,ratio=2,index=3,wave=sine
```

For `fm`, `pm`, `am`, and `ring`, `ratio` derives the modulator frequency from
`freq`, while `modfreq` supplies it directly. A synth cannot explicitly define
or target both.

After a synth declaration, `-w WAVE` is shorthand for setting that synth's
primary `wave` configuration and may be repeated for successive nodes. It is
valid only for `sub`, `fm`, `pm`, `am`, `ring`, and `add`. Without a preceding
`-s`, `-w` retains its legacy meaning.

Targets use either the most recent synth or an explicit ID:

```text
syn.PARAM
syn.ID.PARAM
syn.PARAM.mod
syn.ID.PARAM.mod
```

Unqualified `freq`, `gain`, `pan`, and `gate` continue to target the most
recent synth. A `.mod` inlet is additive; multiple routes to the same inlet
are summed with the direct/base value.

Telemetry and rhythm controls may target any numeric synth parameter. A synth
output may target only parameters reported as `audio-rate=true` by
`stash -i syn.TYPE`.

Each synth has a bipolar, audio-rate, pre-mix, pre-pan output:

```text
syn.ID.out
```

Setting `mix=0` removes a synth from the audible master mix without disabling
that output. Synth outputs use the ordinary modulation grammar:

```bash
stash cpu.usage \
  -s sub:mod,wave=sine,mix=0 \
  -s sub:voice,wave=saw \
  -m freq=80..220 \
  -m syn.mod.out:syn.voice.freq.mod=-300..300
```

The complete graph is validated before Csound starts. Unknown types,
parameters and references, duplicate IDs, unsupported audio-rate targets,
`ratio`/`modfreq` conflicts, missing required configuration, and routing
cycles are errors.

Discovery forms are:

```bash
stash -l syn
stash -i syn.fm
stash -p syn.fm:bass,ratio=2,index=4
```

Granular `size` maps use typed time ranges such as `5ms..150ms`. Numeric maps
and time maps cannot be mixed for a time-valued target.

With explicit synths, `-v` controls master gain (default `1`); each synth's own
`gain` parameter defaults to `0.1`. `-a` supplies the envelope used by every
declared synth.

## 6. Source syntax

Canonical source naming:

```text
domain.component.metric
```

Examples:

```text
cpu.usage
cpu.freq
cpu.temp
cpu.power

cpu.core.0.usage
cpu.core.0.freq

cpu.cores.usage
cpu.cores.freq

gpu.usage
gpu.freq
gpu.temp
gpu.power
gpu.vram

ram.used
ram.free

net.enp4s0.rx
net.enp4s0.tx
net.enp4s0.rx.packets
net.enp4s0.tx.packets

io.nvme0n1.read
io.nvme0n1.write
io.nvme0n1.ops
```

Source names are case-sensitive.

## 7. Scalar and vector sources

Scalar:

```text
cpu.usage
```

produces one number per sample.

Vector:

```text
cpu.cores.usage
```

produces one ordered value per logical CPU.

Vector ordering is stable and corresponds to ascending logical core index.

## 8. Numeric grammar

Plain decimal:

```text
0
1
.1
0.1
12.5
100
```

Magnitude suffixes:

```text
k = 1000
M = 1000000
G = 1000000000
```

Examples:

```text
2k
8k
100M
1G
```

Negative values are valid where the target permits them:

```text
-1
-.5
-12
```

## 9. Time grammar

Supported suffixes:

```text
ms
s
```

Examples:

```text
5ms
150ms
1s
1.5s
```

Bare time values are invalid where a duration is required.

## 10. Range grammar

```text
MIN..MAX
```

Examples:

```text
0..100
80..2k
.05..0.8
-1..1
0..100M
```

`MIN` must be strictly less than `MAX`.

## 11. Mapping grammar

```text
MIN..MAX[/CURVE][~SMOOTH]
```

Examples:

```text
80..2k
80..2k/linear
80..2k/exp
80..2k/log
80..2k~150ms
80..2k/exp~150ms
```

Supported curves:

```text
linear
exp
log
```

Default curve:

```text
linear
```

Default smoothing:

```text
0ms
```

## 12. Modulation grammar

Primary-source modulation:

```text
-m TARGET=MAP
```

Example:

```bash
stash cpu.usage -m freq=80..2k
```

Equivalent logical mapping:

```text
cpu.usage -> freq
```

Explicit control modulation:

```text
-m CONTROL:TARGET=MAP
```

Examples:

```bash
-m cpu.temp:gain=.05..0.3
-m cpu.temp:filter.cutoff=300..8k
-m gpu.usage:delay.feedback=.05..0.8
-m rhythm.gate:filter.cutoff=300..5k
```

`CONTROL` may refer to:

- A telemetry source.
- A rhythm control.
- A synth output such as `syn.mod.out`.

## 13. Modulation targets

Initial signal targets:

```text
freq
gain
pan
gate
```

With explicit synths these address the most recently declared synth. Qualified
synth targets and additive inlets are:

```text
syn.PARAM
syn.ID.PARAM
syn.PARAM.mod
syn.ID.PARAM.mod
```

The valid parameters, units, ranges, and audio-rate support depend on the
synth type and are shown by `stash -i syn.TYPE`.

Effect targets follow one rule:

```text
EFFECT.PARAMETER
```

Every numeric parameter is a legal destination. Current target families are:

```text
filter.cutoff  filter.q  filter.gain
delay.time  delay.feedback  delay.mix  drive.amount
chorus.rate  chorus.depth  chorus.mix
flanger.rate  flanger.depth  flanger.feedback  flanger.mix
phaser.rate  phaser.depth  phaser.feedback  phaser.stages
reverb.size  reverb.damp  reverb.mix
tremolo.rate  tremolo.depth
pan.position  pan.rate  pan.depth  width.amount  haas.delay
crush.bits  crush.rate  shape.drive  shape.bias  fold.amount
comb.delay  comb.feedback  allpass.delay  allpass.feedback
comp.threshold  comp.ratio  comp.attack  comp.release
limiter.threshold  limiter.release
gate.threshold  gate.attack  gate.release
reson.freq  reson.q  ring.freq  ring.mix  freqshift.amount  freqshift.mix
formant.position  pitch.semitones  pitch.mix
stutter.size  stutter.repeats  stutter.prob
grain.size  grain.density  grain.jitter  grain.pitch  grain.mix
freeze.amount  spectral.blur.amount  spectral.blur.mix
spectral.shift.amount  spectral.shift.mix  conv.mix
```

When multiple effects of the same type exist, an unindexed target addresses the most recently declared matching effect.

Example:

```bash
-f lp:2k \
-f lp:4k \
-m cpu.temp:filter.cutoff=300..8k
```

The modulation targets the second low-pass filter.

## 14. Input range override

Primary control:

```text
--range MIN..MAX
```

Example:

```bash
stash net.enp4s0.rx \
  --range 0..100M \
  -m freq=80..2k/log
```

Explicit control:

```text
--range CONTROL=MIN..MAX
```

Example:

```bash
--range net.enp4s0.rx=0..100M
```

Range overrides replace their controls' natural ranges for mapping normalization.
`--range` may be repeated to override several controls independently:

```bash
stash cpu.usage \
  --range cpu.temp=35..90 \
  --range net.enp4s0.rx=0..100M \
  -m cpu.temp:gain=0..1 \
  -m net.enp4s0.rx:freq=80..2k/log
```

## 15. Waveforms

Syntax:

```text
-w WAVE
```

Supported:

```text
sine
square
saw
tri
noise
```

Default:

```text
sine
```

With no synth declaration, `-w` selects the legacy voice waveform. After
`-s`, it sets the most recently declared synth's primary waveform and can be
repeated after later synth declarations. Synths without a primary waveform
reject `-w`; configure wavetable, modal, and granular nodes in `-s` instead.

Example:

```bash
stash cpu.usage -w saw -m freq=80..1k
```

## 16. Static gain

Syntax:

```text
-v GAIN
```

Valid range:

```text
0..1
```

Example:

```bash
-v .2
```

Default without explicit synths:

```text
0.1
```

With explicit synths, `-v` is master gain and defaults to `1`; each node's
`gain` parameter defaults to `0.1`.

## 17. Pan

Pan is exposed as a modulation target.

Range:

```text
-1..1
```

Semantics:

```text
-1 = full left
 0 = center
 1 = full right
```

Example:

```bash
stash net.enp4s0.rx \
  --range 0..100M \
  -m pan=-1..1
```

## 18. Trigger grammar

```text
above:VALUE
below:VALUE
rise:VALUE
fall:VALUE
```

Examples:

```text
above:95
below:20
rise:80
fall:50
```

Semantics:

### `above:X`

The trigger is active for every evaluation where:

```text
value > X
```

### `below:X`

The trigger is active for every evaluation where:

```text
value < X
```

### `rise:X`

Emit one event when:

```text
previous <= X
current  > X
```

### `fall:X`

Emit one event when:

```text
previous >= X
current  < X
```

Vector sources evaluate triggers independently per index.

## 19. Gate duration

Syntax:

```text
-d TIME
```

Example:

```bash
-d 150ms
```

Default for event-driven notes:

```text
100ms
```

## 20. ADSR

Syntax:

```text
-a ATTACK,DECAY,SUSTAIN,RELEASE
```

Example:

```text
-a 5ms,40ms,.7,100ms
```

Constraints:

```text
attack  >= 0ms
decay   >= 0ms
sustain = 0..1
release >= 0ms
```

Default:

```text
5ms,20ms,.8,50ms
```

## 21. Notes

Scientific pitch notation:

```text
LETTER[ACCIDENTAL]OCTAVE
```

Examples:

```text
C4
C#4
Db4
A4
Bb5
```

Supported accidentals:

```text
#
b
```

Default tuning:

```text
A4 = 440Hz
```

Comma-separated arrays:

```text
C4,E4,G4,C5
```

## 22. Scale primitive

Grammar:

```text
scale:ROOT:NAME:LENGTH
```

Examples:

```text
scale:C4:major:8
scale:A3:minor:12
scale:C3:pentatonic-minor:12
```

Supported names:

```text
major
minor
chromatic
pentatonic-major
pentatonic-minor
```

`LENGTH` is a positive integer.

The resolved output contains exactly `LENGTH` notes.

## 23. Mode primitive

Grammar:

```text
mode:ROOT:NAME:LENGTH
```

Examples:

```text
mode:E3:phrygian:12
mode:D4:dorian:8
```

Supported modes:

```text
ionian
dorian
phrygian
lydian
mixolydian
aeolian
locrian
```

`LENGTH` is a positive integer.

The resolved output contains exactly `LENGTH` notes.

## 24. Notes option

Syntax:

```text
-n NOTES
```

`NOTES` accepts:

- One note.
- Comma-separated note array.
- Scale primitive.
- Mode primitive.

Examples:

```bash
-n C4
-n C4,E4,G4,C5
-n scale:C4:major:8
-n mode:E3:phrygian:12
```

For vector sources:

```text
vector index N -> note index N
```

If note count is smaller than vector length, execution fails.

If note count is larger than vector length, extra notes are ignored.

## 25. Rhythm primitive

Full grammar:

```text
rhythm:BPM:DIVISION:PATTERN
```

Tempo-omitted grammar:

```text
rhythm:DIVISION:PATTERN
```

Examples:

```text
rhythm:120:1/4:xxxx
rhythm:120:1/8:x-x-x-x-
rhythm:140:1/16:x---x---x-x-x---
rhythm:1/8:x-x-x-x-
```

When BPM is omitted, `-b BPM` is required.

## 26. BPM

Syntax:

```text
-b BPM
```

Valid BPM:

```text
> 0
```

Examples:

```bash
-b 90
-b 120
-b 172.5
```

If both `-b` and rhythm-embedded BPM are provided, `-b` overrides the rhythm BPM.

## 27. Rhythm division

Grammar:

```text
1/N
```

Initial supported divisions:

```text
1/1
1/2
1/4
1/8
1/16
1/32
```

Division identifies the duration represented by one pattern step.

## 28. Rhythm pattern

Initial alphabet:

```text
x = hit
- = rest
```

Examples:

```text
xxxx
x-x-x-x-
x---x---x-x-x---
```

Pattern must contain at least one step.

## 29. Rhythm controls

Every active rhythm exposes:

```text
rhythm.gate
rhythm.hit
rhythm.step
rhythm.velocity
rhythm.phase
```

Definitions:

### `rhythm.gate`

```text
1 on hit steps
0 on rest steps
```

### `rhythm.hit`

A one-evaluation pulse at the beginning of each `x` step.

### `rhythm.step`

Zero-based current pattern step index.

### `rhythm.velocity`

```text
1.0 on x
0.0 on -
```

### `rhythm.phase`

Normalized progress through the current step:

```text
0..1
```

## 30. Swing

Syntax:

```text
--swing AMOUNT
```

Range:

```text
50..75
```

Default:

```text
50
```

`50` means straight timing.

Swing applies to alternating subdivisions.

## 31. Rhythm option

Syntax:

```text
-r RHYTHM
```

Examples:

```bash
-r rhythm:120:1/8:x-x-x-x-
```

```bash
-b 120 \
-r rhythm:1/8:x-x-x-x-
```

A rhythm articulates event-driven sound and exposes rhythm controls for modulation.

## 32. Filters

Syntax:

```text
-f FILTER
```

### Low-pass

```text
lp:CUTOFF
lp:CUTOFF,Q
```

Examples:

```bash
-f lp:2k
-f lp:2k,.7
```

### High-pass

```text
hp:CUTOFF
hp:CUTOFF,Q
```

Examples:

```bash
-f hp:80
-f hp:80,.7
```

Default Q:

```text
0.707
```

Filters are appended in declaration order.

## 33. Generic effects

Syntax:

```text
-x EFFECT
```

Both positional and named arguments are accepted. The effect set is:

```text
delay:TIME,FEEDBACK,MIX              drive:AMOUNT
chorus:RATE,DEPTH,MIX                flanger:RATE,DEPTH,FEEDBACK[,MIX]
phaser:RATE,DEPTH,STAGES[,FEEDBACK]  reverb:SIZE,DAMP,MIX
tremolo:RATE,DEPTH                   pan:POSITION
autopan:RATE,DEPTH                   width:AMOUNT  haas:DELAY
crush:BITS,RATE                      bitcrush:BITS  downsample:RATE
shape:DRIVE,BIAS                     fold:AMOUNT
comb:DELAY,FEEDBACK                  allpass:DELAY,FEEDBACK
comp:THRESHOLD,RATIO,ATTACK,RELEASE  limiter:THRESHOLD[,RELEASE]
gate:THRESHOLD,ATTACK,RELEASE        reson:FREQ,Q
ring:FREQ,MIX                        freqshift:AMOUNT[,MIX]
formant:POSITION                     pitch:SEMITONES[,MIX]
stutter:SIZE,REPEATS,PROB            grain:SIZE,DENSITY,JITTER,PITCH,MIX
freeze:AMOUNT                        spectral.freeze:AMOUNT
spectral.blur:AMOUNT[,MIX]
spectral.shift:AMOUNT[,MIX]          conv:IMPULSE[,MIX]
```

Named forms are recommended when an effect has several controls:

```bash
-x chorus:rate=.8,depth=.3,mix=.25
-x flanger:rate=.2,depth=5ms,feedback=.4
-x comp:threshold=-12db,ratio=4,attack=5ms,release=80ms
-x formant:vowel=a
-x pitch:ratio=1.5
```

Waveshaper curves are `tanh`, `clip`, and `atan`. Formant vowels are `a`, `e`,
`i`, `o`, and `u`. `bitcrush`, `downsample`, `autopan`, and `waveshaper` are
convenience aliases. Effects are appended and processed in declaration order.

Effects are appended in declaration order.

## 34. Output

Default:

```text
audio device
```

Raw PCM:

```text
-o -
```

Initial raw PCM format:

```text
sample rate: 48000 Hz
channels:    2
sample type: float32
endianness:  little-endian
interleave:  stereo interleaved
```

No metadata header is written.

## 35. stdin source

Source:

```text
-
```

Format:

```text
one numeric sample per non-empty line
```

Example:

```text
0.1
0.4
0.8
0.2
```

Empty lines are ignored.

Invalid non-empty lines terminate execution with an error containing the line number.

Example:

```bash
producer |
stash - \
  --range 0..1 \
  -m freq=100..2k
```

## 36. stdout contract

### Telemetry mode

stdout:

```text
numeric telemetry samples
```

stderr:

```text
diagnostics
```

### Audio device mode

stdout:

```text
empty
```

stderr:

```text
diagnostics
```

### Raw PCM mode

stdout:

```text
raw PCM
```

stderr:

```text
diagnostics
```

## 37. Telemetry output format

Scalar source:

```text
VALUE\n
```

Example:

```text
42.7
```

Vector source:

```text
VALUE,VALUE,VALUE,...\n
```

Example:

```text
12.1,100,7.2,83.4
```

No labels are emitted in telemetry mode.

## 38. Discovery

### List all sources

```bash
stash -l
```

### Filter source list

```bash
stash -l cpu
```

Filtering is prefix-based.

### List synths

```bash
stash -l syn
```

Synth discovery names use the `syn.TYPE` namespace.

### Inspect source

```bash
stash -i cpu.usage
```

Human-readable output contains:

```text
name
kind
unit
natural range
availability
```

### Inspect synth type

```bash
stash -i syn.fm
```

Human-readable output contains the description, fixed configuration,
modulatable parameters with units/ranges/defaults and audio-rate support, and
the `out` audio output.

### Resolve primitive

```bash
stash -p C4
stash -p scale:C4:major:8
stash -p mode:E3:phrygian:8
stash -p rhythm:120:1/8:x-x-x-x-
stash -p syn.fm:bass,ratio=2,index=4
```

A resolved synth declaration includes its assigned ID, configuration, and all
numeric base values.

## 39. Defaults

```text
waveform       sine
frequency      440Hz
voice/node gain 0.1
synth master   1
pan            0
curve          linear
smoothing      0ms
event gate     100ms
ADSR           5ms,20ms,.8,50ms
filter Q       .707
swing          50
sample rate    48000Hz
channels       2
```

## 40. Canonical errors

Malformed syntax fails immediately.

Examples:

```text
stash: unknown source: cpu.foobar
stash: invalid range: 80...2k
stash: invalid map: freq=80..2k/cubic
stash: unknown curve: cubic
stash: invalid trigger: over:95
stash: invalid note: H4
stash: unknown mode: superlocrian
stash: invalid rhythm pattern: x_o_x
stash: unknown synth: supersaw
stash: duplicate synth id: bass
stash: source cpu.power unavailable on this system
stash: 12 vector values require at least 12 notes; got 8
```

No silent fallback is permitted for malformed syntax.

## 41. Live instrument editor

Running `stash` with no arguments opens a full-screen editor. Each non-empty
line is exactly one clause of the ordinary CLI language: the first clause is a
source and each remaining clause is one `OPTION VALUE` pair. Empty lines are
temporary editing placeholders. The editor continuously compiles the lines to
the same argv parser and planner used by argument-bearing invocations.

Document states are:

```text
✓ valid       complete audio instrument
… incomplete  useful prefix or a command without sound clauses
✕ invalid     malformed or semantically invalid command
```

The last valid instrument keeps playing while text is incomplete or invalid.
Changing numeric synth or effect parameters updates the persistent audio
session without resetting oscillator phase. Source, control/routing, synth or
effect topology, synth type, and fixed-configuration changes rebuild the graph
inside the same STASH process.

The first bare source clause is the primary telemetry control. A second bare
source clause is not valid syntax; additional telemetry sources are selected in
repeated mappings such as `-m gpu.usage:syn.voice.gain=.02...2`. Omitting the
control and colon, as in `-m syn.voice.freq=80..800`, uses the primary source.
Telemetry sources are control-rate values and may map into any numeric synth or
effect parameter. They are not audio-rate synth signals. Only `syn.ID.out` may
appear as an audio-rate control, and it may target only parameters reported as
audio-rate capable.

The editor chrome is neutral. Stable semantic colors identify telemetry sources
and synth IDs across their declaration and route occurrences. The inspector
also renders every selected mapping as a labeled source-to-target relationship
and distinguishes control-rate maps from audio-rate routes, so color is not the
only carrier of meaning.

Normal-mode keys:

```text
Up / Down       select clause
Enter           edit clause
a               add clause below
Ctrl+Shift+D    delete clause
Ctrl+Up/Down    reorder clause
Ctrl+N          add clause below
Ctrl+P          add clause above
Ctrl+M          mute or unmute the instrument
Ctrl+G          export valid instrument and exit
q / Ctrl+C      quit
```

Terminals that cannot distinguish modified control characters use `Alt+D` for
delete and `Alt+M` for mute/unmute; the editor help bar shows the active keys.

Edit-mode keys:

```text
Tab             cycle semantic suggestions
Up / Down       select suggestion
Enter           accept suggestion, or finish when none is shown
Ctrl+D          finish editing
Left / Right    select numeric values, or move the cursor when none exist
Alt+Up/Down     nudge the selected numeric value
Ctrl+Shift+D    delete the current clause
Ctrl+M          mute or unmute the instrument
Ctrl+N/P        add a clause below/above
```

Completion is derived from registered sources and structured synth/effect
metadata. Declared synth IDs, synth outputs, rhythm controls, effects, and only
their valid parameter targets enter the relevant completion contexts. Numeric
options and parameters with documented defaults insert those defaults when
their completion is accepted. The complete numeric token remains selected so
the next typed or pasted value replaces the default instead of appending to it.

Export uses the ordinary shell-safe form:

```bash
stash cpu.usage \
  -s fm:bass,ratio=2,index=7 \
  -m syn.bass.freq=45..90/exp~120ms
```
