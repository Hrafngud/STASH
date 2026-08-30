# STASH

STASH — Sound Telemetry Auto SHell — is a Linux-first command-line tool for turning live hardware and operating-system telemetry into sound.

```bash
stash cpu.usage -w sine -m freq=80..2k/exp~150ms
```

The CLI is the instrument: telemetry sources control persistent signals and ordered effects through short, shell-safe commands. Csound is used internally as the audio backend.

## Status

STASH is under active development. The implementation is divided into tracked slices in [`TASKS.md`](TASKS.md); the unit and range parsing foundation is currently complete.

The intended first release includes CPU, RAM, network, disk, and available GPU telemetry; notes, scales, modes, and rhythms; common oscillator waveforms; filters, delay, and drive; device audio; and raw PCM output.

## Requirements

- Linux
- Go 1.23 or newer
- Csound for audio features
- Codex CLI only when using the automated development loop

## Development

Run the currently implemented tests:

```bash
go test ./...
go vet ./...
```

Run a bounded number of implementation slices non-interactively:

```bash
./codex-loop.sh 5
```

Each Codex invocation works on exactly one dependency-ready slice and records its result in `TASKS.md`.

## Documentation

- [`SYNTAX.md`](SYNTAX.md) — authoritative CLI syntax
- [`EXAMPLES.md`](EXAMPLES.md) — intended command examples
- [`PLANNING.md`](PLANNING.md) — architecture and scope
- [`AGENT.md`](AGENT.md) — engineering rules
- [`TASKS.md`](TASKS.md) — implementation status and handoffs
