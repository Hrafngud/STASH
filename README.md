# STASH

**Turn your Linux machine into a playable instrument.**

STASH (Sound Telemetry Auto SHell) is a Linux-first command-line tool that
maps live system activity—CPU load, temperature, memory, disk, network, and
GPU telemetry—to sound. Use it to hear a build finish, turn network traffic
into a drone, create a performance patch from hardware sensors, or simply
explore what your computer is doing without watching another graph.

```bash
stash cpu.usage -w sine -m freq=80..2k/exp~150ms
```

The command above reads total CPU usage, maps it exponentially from 80 Hz to
2 kHz, smooths changes over 150 ms, and sends the result to a sine oscillator.
Csound handles audio behind the scenes; STASH gives it a discoverable,
shell-friendly interface and an optional live terminal editor.

## What you can do

- Read telemetry as plain numeric output, with no audio engine required.
- Sonify CPU, memory, network, disk, AMD/NVIDIA GPU, or newline-delimited
  values from another command.
- Build patches from subtractive, FM, PM, AM, ring, additive, wavetable,
  Karplus–Strong, modal, and granular synths.
- Map several telemetry sources to synth and effect parameters at once.
- Add notes, scales, modes, triggers, rhythms, envelopes, filters, and an
  ordered chain of modulatable effects.
- Route one synth into another at audio rate for modular patches.
- Create, hear, edit, and save instruments in the full-screen terminal UI.
- Stream raw 48 kHz stereo float32 PCM into PipeWire or another audio tool.

STASH detects sources at runtime. Unsupported or unreadable hardware remains
visible in discovery output with an explanation instead of silently
disappearing.

## Requirements

- Linux with readable `/proc` and `/sys` telemetry interfaces
- [Go](https://go.dev/) 1.25 or newer to build from source
- [Csound](https://csound.com/) 6 or newer for live audio and raw PCM output

Csound is optional if you only want telemetry, discovery, or primitive
resolution. GPU metrics depend on the interfaces exposed by your hardware and
driver; STASH supports AMD through sysfs and NVIDIA through NVML when present.

## Install

### Arch Linux and Manjaro

Clone the repository and run the included user-local installer:

```bash
git clone https://github.com/zalmo/stash.git
cd stash
./install.sh
```

The script installs missing packages with `pacman`, builds STASH, and installs
the binary and manual under `~/.local`. If necessary, add the binary directory
to your shell configuration:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Other Linux distributions

Install Go and Csound with your distribution's package manager, then build and
install STASH from the repository:

```bash
git clone https://github.com/zalmo/stash.git
cd stash
go build -o stash ./cmd/stash
install -Dm755 stash "$HOME/.local/bin/stash"
install -Dm644 docs/stash.1 "$HOME/.local/share/man/man1/stash.1"
```

Confirm that the installation is available:

```bash
stash --help
stash -l cpu
```

If `stash` is not found, add `~/.local/bin` to `PATH` as shown above. If the
manual is not found immediately, run `mandb` or start a new shell.

## First instrument: a short walkthrough

Start by asking STASH what it can read on your machine:

```bash
stash -l
```

The output includes each source's unit, value shape, and availability. Inspect
one source for its natural range and any hardware-specific details:

```bash
stash -i cpu.usage
```

Read it without generating sound:

```bash
stash cpu.usage
```

Now turn it into pitch. Start with a low system volume, then run:

```bash
stash cpu.usage -w sine -m freq=80..2k/exp~150ms
```

The command follows a small mental model:

```text
telemetry source  ->  mapping  ->  synth/effect parameter
cpu.usage         ->  80..2k   ->  frequency
```

- `cpu.usage` selects the live control source.
- `-w sine` enables a sine-wave voice.
- `-m freq=80..2k/exp~150ms` maps the source to pitch, uses an exponential
  curve, and smooths abrupt changes.

Add a filter and effects to shape the result:

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1k/exp~100ms \
  -f lp:3k \
  -x drive:.2 \
  -x reverb:size=.7,damp=.4,mix=.25
```

Press `Ctrl+C` to stop any live telemetry or audio command.

## Live editor

Run STASH with no arguments to open the terminal instrument editor:

```bash
stash
```

The editor starts with a playable patch and teaches the same syntax used by
the CLI. Completions are aware of the sources available on your machine and
the parameters valid for each synth or effect. The last valid patch keeps
playing while you edit, and numeric changes update the current audio session.

Useful controls:

| Key | Action |
| --- | --- |
| `Enter` | Edit the selected clause |
| `a` or `Ctrl+N` | Add a clause below |
| `Tab` | Cycle context-aware suggestions while editing |
| `Alt+Up` / `Alt+Down` | Nudge the selected numeric value |
| `Ctrl+M` | Mute or unmute |
| `Ctrl+O` | Toggle the ASCII oscillator display |
| `Ctrl+G` | Save the current instrument as a preset |
| `Ctrl+Shift+G` | Load a preset |
| `q` or `Ctrl+C` | Quit |

Presets are readable `.stash` commands stored in
`$XDG_CONFIG_HOME/stash/presets`, or `~/.config/stash/presets` by default. You
can also paste a complete one-line or `\`-continued `stash` command into the
editor to replace the current patch.

## Going further

### Use several telemetry sources

Keep the primary source at the start of the command and name additional
sources on their mappings:

```bash
stash cpu.usage \
  -s fm:motion,ratio=2,index=3 \
  -m syn.motion.freq=80..800/log~100ms \
  -m cpu.temp:syn.motion.index=.5..8 \
  -m gpu.usage:syn.motion.gain=.02...2
```

The bare mapping uses the primary `cpu.usage` source. The other mappings read
CPU temperature and GPU usage independently. Run `stash -l` first: optional
hardware sources may not be available on every machine.

### Feed STASH from another command

Use `-` as the source and provide the expected input range:

```bash
printf '0\n.25\n.5\n.75\n1\n' |
  stash - --range 0..1 -m freq=100..2k
```

STASH accepts one finite number per non-empty input line, so scripts and Unix
pipelines can become control sources.

### Stream raw audio

`-o -` writes headerless, stereo-interleaved, little-endian float32 PCM at
48,000 Hz. For example, send it to PipeWire:

```bash
stash cpu.usage -m freq=80..2k/exp~150ms -o - |
  pw-cat --playback --rate 48000 --channels 2 --format f32
```

Diagnostics stay on stderr, so telemetry values and raw audio on stdout remain
safe to pipe.

## Command guide

```text
stash                         open the live editor
stash SOURCE [OPTIONS]        read or sonify a source
stash -l [PREFIX]             discover sources; use "syn" for synths
stash -i NAME                 inspect a source or synth
stash -p PRIMITIVE            resolve a note, scale, rhythm, or synth
stash -h | --help             show the command summary
```

For the complete grammar and more ready-to-run patches, see:

- [`SYNTAX.md`](SYNTAX.md) — authoritative command syntax and semantics
- [`EXAMPLES.md`](EXAMPLES.md) — examples organized by capability
- [`docs/stash.1`](docs/stash.1) — source for the installed `stash(1)` manual

## Development

Run the project checks from the repository root:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/stash
```

## License

No license has been selected yet.
