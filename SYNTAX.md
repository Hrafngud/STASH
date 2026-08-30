# SYNTAX.md

## 1. Command form

```text
stash SOURCE [OPTIONS]
stash DISCOVERY
```

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
-l [PREFIX]                  list sources
-i SOURCE                    inspect a source
-p PRIMITIVE                 resolve/inspect a primitive

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

Repeated `-m`, `-f`, and `-x` options are allowed.

Repeated effects preserve command-line order.

## 4. Operating modes

### 4.1 Telemetry mode

A source with no audio-producing options:

```bash
stash cpu.usage
```

writes samples to stdout.

The following options activate audio mode:

```text
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

## 5. Source syntax

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
ram.pressure

net.enp4s0.rx
net.enp4s0.tx
net.enp4s0.rx.packets
net.enp4s0.tx.packets

io.nvme0n1.read
io.nvme0n1.write
io.nvme0n1.ops
```

Source names are case-sensitive.

## 6. Scalar and vector sources

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

## 7. Numeric grammar

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

## 8. Time grammar

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

## 9. Range grammar

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

## 10. Mapping grammar

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

## 11. Modulation grammar

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

## 12. Modulation targets

Initial signal targets:

```text
freq
gain
pan
gate
```

Initial filter targets:

```text
filter.cutoff
filter.q
```

Initial delay targets:

```text
delay.time
delay.feedback
delay.mix
```

Initial drive target:

```text
drive.amount
```

When multiple effects of the same type exist, an unindexed target addresses the most recently declared matching effect.

Example:

```bash
-f lp:2k \
-f lp:4k \
-m cpu.temp:filter.cutoff=300..8k
```

The modulation targets the second low-pass filter.

## 13. Input range override

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

A range override replaces the source's natural range for mapping normalization.

## 14. Waveforms

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

Example:

```bash
stash cpu.usage -w saw -m freq=80..1k
```

## 15. Static gain

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

Default:

```text
0.1
```

## 16. Pan

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

## 17. Trigger grammar

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

## 18. Gate duration

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

## 19. ADSR

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

## 20. Notes

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

## 21. Scale primitive

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

## 22. Mode primitive

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

## 23. Notes option

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

## 24. Rhythm primitive

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

## 25. BPM

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

## 26. Rhythm division

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

## 27. Rhythm pattern

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

## 28. Rhythm controls

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

## 29. Swing

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

## 30. Rhythm option

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

## 31. Filters

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

## 32. Generic effects

Syntax:

```text
-x EFFECT
```

### Delay

```text
delay:TIME,FEEDBACK,MIX
```

Example:

```bash
-x delay:150ms,.4,.25
```

Constraints:

```text
TIME > 0
FEEDBACK = 0..0.95
MIX = 0..1
```

### Drive

```text
drive:AMOUNT
```

Example:

```bash
-x drive:.5
```

Constraint:

```text
AMOUNT = 0..1
```

Effects are appended in declaration order.

## 33. Output

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

## 34. stdin source

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

## 35. stdout contract

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

## 36. Telemetry output format

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

## 37. Discovery

### List all sources

```bash
stash -l
```

### Filter source list

```bash
stash -l cpu
```

Filtering is prefix-based.

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

### Resolve primitive

```bash
stash -p C4
stash -p scale:C4:major:8
stash -p mode:E3:phrygian:8
stash -p rhythm:120:1/8:x-x-x-x-
```

## 38. Defaults

```text
waveform       sine
frequency      440Hz
gain           0.1
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

## 39. Canonical errors

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
stash: source cpu.power unavailable on this system
stash: 12 vector values require at least 12 notes; got 8
```

No silent fallback is permitted for malformed syntax.
