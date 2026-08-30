# EXAMPLES.md

## 1. Read telemetry

Aggregate CPU usage:

```bash
stash cpu.usage
```

Per-core CPU usage:

```bash
stash cpu.cores.usage
```

CPU temperature:

```bash
stash cpu.temp
```

Network receive throughput:

```bash
stash net.enp4s0.rx
```

Write telemetry to a file:

```bash
stash cpu.temp > cpu-temp.log
```

Pipe telemetry into another Unix program:

```bash
stash cpu.usage | awk '{sum += $1; n++} END {print sum / n}'
```

## 2. Discover sources

List all available source names:

```bash
stash -l
```

List CPU-related sources:

```bash
stash -l cpu
```

Inspect aggregate CPU usage:

```bash
stash -i cpu.usage
```

Inspect per-core vector usage:

```bash
stash -i cpu.cores.usage
```

## 3. Resolve musical primitives

Resolve middle C:

```bash
stash -p C4
```

Resolve a major scale:

```bash
stash -p scale:C4:major:8
```

Resolve E Phrygian:

```bash
stash -p mode:E3:phrygian:12
```

Inspect a rhythm:

```bash
stash -p rhythm:120:1/8:x-x-x-x-
```

## 4. CPU sine glide

The core STASH example:

```bash
stash cpu.usage \
  -w sine \
  -m freq=80..2k/exp~150ms
```

Behavior:

```text
CPU usage -> exponential pitch mapping -> smoothed sine frequency
```

Low CPU produces low pitch.

High CPU produces high pitch.

The oscillator remains continuous while frequency changes.

## 5. Fast CPU theremin

```bash
stash cpu.usage \
  -w sine \
  -m freq=100..1500/exp~20ms
```

## 6. Slow CPU drone

```bash
stash cpu.usage \
  -w sine \
  -m freq=40..500/exp~1s \
  -v .15
```

## 7. CPU saw oscillator

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1200/exp~100ms
```

## 8. CPU square oscillator

```bash
stash cpu.usage \
  -w square \
  -m freq=60..800/exp~100ms \
  -v .05
```

## 9. CPU frequency drives sound frequency

```bash
stash cpu.freq \
  -m freq=100..1200/linear~100ms
```

STASH uses the detected natural CPU frequency range unless `--range` overrides it.

## 10. Temperature drives pitch

```bash
stash cpu.temp \
  --range 30..90 \
  -w sine \
  -m freq=100..900/exp~500ms
```

## 11. CPU usage controls pan

```bash
stash cpu.usage \
  -w sine \
  -m freq=220..440 \
  -m pan=-1..1
```

At low CPU usage the signal is left.

At high CPU usage the signal is right.

## 12. CPU harmonica

Map each logical CPU to one note.

```bash
stash cpu.cores.usage \
  -t above:95 \
  -n C4,D4,E4,G4,A4,C5,D5,E5,G5,A5,C6,D6 \
  -d 150ms \
  -w sine
```

For a 12-core vector:

```text
core 0  -> C4
core 1  -> D4
core 2  -> E4
core 3  -> G4
core 4  -> A4
core 5  -> C5
core 6  -> D5
core 7  -> E5
core 8  -> G5
core 9  -> A5
core 10 -> C6
core 11 -> D6
```

## 13. CPU harmonica using a scale

```bash
stash cpu.cores.usage \
  -t above:95 \
  -n scale:C4:major:12 \
  -d 150ms \
  -w sine
```

## 14. Phrygian CPU organ

```bash
stash cpu.cores.usage \
  -t above:95 \
  -n mode:E3:phrygian:12 \
  -d 150ms \
  -w sine
```

## 15. Edge-triggered core notes

Only play a note when a core crosses upward through 95%:

```bash
stash cpu.cores.usage \
  -t rise:95 \
  -n mode:E3:phrygian:12 \
  -d 150ms
```

## 16. CPU harmonica with ADSR

```bash
stash cpu.cores.usage \
  -t above:95 \
  -n mode:E3:phrygian:12 \
  -d 150ms \
  -a 5ms,40ms,.7,120ms
```

## 17. Basic rhythm

```bash
stash cpu.usage \
  -r rhythm:120:1/8:x-x-x-x- \
  -m freq=100..1k/exp
```

CPU usage determines pitch.

The rhythm determines articulation.

## 18. Separate BPM

Equivalent rhythm with tempo outside the primitive:

```bash
stash cpu.usage \
  -b 120 \
  -r rhythm:1/8:x-x-x-x- \
  -m freq=100..1k/exp
```

## 19. Sixteenth-note telemetry melody

```bash
stash cpu.cores.usage \
  -b 110 \
  -r rhythm:1/16:xxxxxxxxxxxxxxxx \
  -t above:95 \
  -n scale:C3:pentatonic-minor:12 \
  -d 80ms
```

At each rhythmic hit STASH observes the per-core activity and maps positions above 95% into the scale.

## 20. Sparse rhythm

```bash
stash cpu.usage \
  -r rhythm:90:1/16:x---x---x-x-x--- \
  -w sine \
  -m freq=80..900/exp
```

## 21. Swing

```bash
stash cpu.usage \
  -b 110 \
  -r rhythm:1/8:x-x-x-x- \
  --swing 58 \
  -m freq=100..800
```

## 22. Low-pass filter

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1k/exp \
  -f lp:2k
```

## 23. Low-pass with Q

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1k/exp \
  -f lp:2k,.9
```

## 24. High-pass and low-pass

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1k \
  -f hp:60 \
  -f lp:4k
```

Effect/filter order is the declaration order.

## 25. Temperature controls filter cutoff

```bash
stash cpu.usage \
  --range cpu.temp=30..90 \
  -w saw \
  -m freq=80..1200/exp~100ms \
  -f lp:2k \
  -m cpu.temp:filter.cutoff=300..8k/exp~300ms
```

CPU usage controls oscillator pitch.

CPU temperature controls the low-pass cutoff.

## 26. Drive

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..900 \
  -x drive:.35
```

## 27. Delay

```bash
stash cpu.usage \
  -w sine \
  -m freq=100..1k \
  -x delay:150ms,.4,.25
```

## 28. Drive into delay

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..900 \
  -x drive:.25 \
  -x delay:120ms,.3,.2
```

Signal chain:

```text
saw -> drive -> delay -> output
```

## 29. GPU usage controls delay feedback

```bash
stash cpu.usage \
  -w sine \
  -m freq=100..1k \
  -x delay:150ms,.1,.25 \
  -m gpu.usage:delay.feedback=.05..0.8
```

## 30. Rhythm controls filter cutoff

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1k/exp~100ms \
  -f lp:300 \
  -r rhythm:120:1/8:x-x-x-x- \
  -m rhythm.gate:filter.cutoff=300..5k
```

The oscillator pitch continuously follows CPU usage.

The filter opens on rhythm hits.

## 31. Rhythm phase controls pan

```bash
stash cpu.usage \
  -w sine \
  -m freq=200..800 \
  -r rhythm:120:1/4:xxxx \
  -m rhythm.phase:pan=-1..1
```

Each rhythmic step sweeps from left to right.

## 32. Rhythm velocity controls gain

```bash
stash cpu.usage \
  -w sine \
  -m freq=200..800 \
  -r rhythm:120:1/8:x-x-x-x- \
  -m rhythm.velocity:gain=.02..0.2
```

## 33. Network throughput as pitch

```bash
stash net.enp4s0.rx \
  --range 0..100M \
  -w sine \
  -m freq=80..2k/log~50ms
```

## 34. Network throughput as pan

```bash
stash net.enp4s0.rx \
  --range 0..100M \
  -w sine \
  -m freq=440..880 \
  -m pan=-1..1
```

## 35. Receive and CPU combined

```bash
stash cpu.usage \
  -w saw \
  -m freq=80..1k/exp \
  -m net.enp4s0.rx:gain=.02..0.25 \
  --range net.enp4s0.rx=0..100M
```

CPU controls pitch.

Network receive throughput controls gain.

## 36. CPU temperature and GPU usage as effect controls

```bash
stash cpu.usage \
  --range cpu.temp=30..90 \
  -w saw \
  -m freq=80..1200/exp~100ms \
  -f lp:2k \
  -m cpu.temp:filter.cutoff=300..8k~300ms \
  -x delay:120ms,.1,.2 \
  -m gpu.usage:delay.feedback=.05..0.8
```

## 37. stdin as a source

Given:

```bash
printf '0\n0.25\n0.5\n0.75\n1\n'
```

Sonify it:

```bash
for value in 0 0.25 0.5 0.75 1; do
  printf '%s\n' "$value"
  sleep .15
done |
stash - \
  --range 0..1 \
  -w sine \
  -m freq=100..2k/exp
```

## 38. Shell-generated telemetry

```bash
while sleep .1; do
  awk 'BEGIN{srand(); print rand()}'
done |
stash - \
  --range 0..1 \
  -m freq=100..1500
```

## 39. Pipe arbitrary numeric processing into STASH

```bash
stash cpu.usage |
awk '{print $1 / 100; fflush()}' |
stash - \
  --range 0..1 \
  -m freq=80..2k/exp
```

This round-trip is intentionally valid.

## 40. Raw PCM to PipeWire

```bash
stash cpu.usage \
  -w sine \
  -m freq=80..2k/exp~150ms \
  -o - |
pw-cat \
  --playback \
  --raw \
  --rate 48000 \
  --channels 2 \
  --format f32 \
  -
```

## 41. Raw PCM to a file

```bash
stash cpu.usage \
  -w sine \
  -m freq=80..2k \
  -o - > cpu.raw
```

The file is headerless float32 little-endian stereo PCM at 48 kHz.

## 42. Test core affinity with the CPU harmonica

Run STASH:

```bash
stash cpu.cores.usage \
  -t above:95 \
  -n mode:E3:phrygian:12 \
  -d 150ms
```

In another shell:

```bash
taskset -c 0 stress -c 1
```

Then:

```bash
taskset -c 3 stress -c 1
```

Then:

```bash
taskset -c 0,2,4 stress -c 3
```

Each selected logical CPU maps to its corresponding mode note.

## 43. Full CPU load chord

```bash
taskset -c 0-11 stress -c 12
```

With:

```bash
stash cpu.cores.usage \
  -t above:95 \
  -n scale:C3:pentatonic-minor:12 \
  -d 150ms
```

all active cores generate their mapped notes.

## 44. Minimal commands worth keeping in shell history

Read CPU:

```bash
stash cpu.usage
```

CPU glide:

```bash
stash cpu.usage -m freq=80..2k/exp~150ms
```

CPU harmonica:

```bash
stash cpu.cores.usage -t above:95 -n mode:E3:phrygian:12 -d 150ms
```

Rhythmic CPU:

```bash
stash cpu.usage -r rhythm:120:1/8:x-x-x-x- -m freq=100..1k
```

Filtered CPU:

```bash
stash cpu.usage -w saw -m freq=80..1k -f lp:2k
```

Unix input:

```bash
while sleep .1; do printf '.5\n'; done |
stash - --range 0..1 -m freq=100..2k
```
