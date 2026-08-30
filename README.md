# STASH

STASH — Sound Telemetry Auto SHell — is a Linux-first command-line instrument that turns live hardware and operating-system telemetry into sound.

```bash
stash cpu.usage -w sine -m freq=80..2k/exp~150ms
```

Telemetry sources control persistent oscillators and ordered effects through a small, shell-safe CLI. Csound is the private synthesis backend.

## Requirements

- Linux with readable `/proc` and `/sys` telemetry interfaces
- Go 1.23 or newer to build
- Csound 6 or newer for device audio and raw PCM output

Telemetry and discovery commands do not require Csound. Hardware-specific sources are reported as unavailable when STASH cannot detect a reliable local interface.

## Build

```bash
go build -o stash ./cmd/stash
```

Put the resulting `stash` binary on `PATH`, or run it as `./stash` from the repository.

## Quick start

Read aggregate CPU usage as machine-oriented numeric lines:

```bash
stash cpu.usage
```

Discover and inspect sources:

```bash
stash -l
stash -l cpu
stash -i cpu.usage
```

Resolve musical primitives:

```bash
stash -p C4
stash -p mode:E3:phrygian:12
stash -p rhythm:120:1/8:x-x-x-x-
```

Sonify CPU usage through the default audio device:

```bash
stash cpu.usage -w saw -m freq=80..1k/exp~100ms -f lp:3k -x drive:.2
```

Use newline-delimited numbers from stdin:

```bash
printf '0\n.25\n.5\n.75\n1\n' |
stash - --range 0..1 -m freq=100..2k
```

Press Ctrl-C to stop a live telemetry or audio command cleanly. Diagnostics go to stderr; telemetry data and raw audio never share stdout with status text.

## Raw PCM

`-o -` writes headerless audio to stdout with this fixed format:

- 48,000 Hz
- two channels
- stereo interleaved
- little-endian float32 samples

For example:

```bash
stash cpu.usage -m freq=80..2k/exp~150ms -o - |
pw-cat --playback --rate 48000 --channels 2 --format f32
```

## Development

Run the release checks with:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/stash
```

The bounded implementation loop and slice history are documented in [`TASKS.md`](TASKS.md).

## Documentation

- [`SYNTAX.md`](SYNTAX.md) — authoritative CLI syntax
- [`EXAMPLES.md`](EXAMPLES.md) — command examples
- [`PLANNING.md`](PLANNING.md) — architecture and scope
- [`AGENT.md`](AGENT.md) — engineering rules
- [`TASKS.md`](TASKS.md) — implementation status and handoffs

## License

The repository owner has not selected a license yet.
