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

Resolve an accidental and a note array:

```bash
stash -p Db4
stash -p C4,E4,G4,C5
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

## 45. Discover RAM and disk I/O sources

RAM source names are stable:

```bash
stash -l ram
stash -i ram.used
stash -i ram.free
```

Disk source names include the kernel device name, so discover the names on the
current machine before copying an I/O example:

```bash
stash -l io
stash -i io.nvme0n1.read
```

Replace `nvme0n1` below when `stash -l io` reports a different device.

## 46. Read RAM telemetry

Used memory in bytes:

```bash
stash ram.used
```

Readily available memory in bytes:

```bash
stash ram.free
```

Both sources have a detected natural range from zero to total physical RAM.

## 47. RAM usage as a triangle drone

```bash
stash ram.used \
  -w tri \
  -m freq=55..660/log~1s \
  -m gain=.03..0.18
```

Used RAM controls pitch and loudness. No `--range` is needed because STASH
detects the machine's total memory.

## 48. Low-memory warning notes

Hold a low note while available RAM remains below 2 GB:

```bash
stash ram.free \
  -t below:2G \
  -n C2 \
  -d 500ms \
  -a 5ms,40ms,.8,250ms
```

Play one note only when available RAM crosses downward through 2 GB:

```bash
stash ram.free \
  -t fall:2G \
  -n C2 \
  -d 500ms
```

Together with the `above` and `rise` CPU examples, these commands cover all
four trigger forms.

## 49. RAM as filtered noise

```bash
stash ram.used \
  -w noise \
  -m gain=.01..0.12~500ms \
  -f hp:120 \
  -f lp:4k
```

This also demonstrates the `noise` waveform; the RAM drone above demonstrates
`tri`. The earlier examples cover `sine`, `square`, and `saw`.

## 50. Read disk I/O telemetry

Read and write throughput are emitted in bytes per second. Operations are
emitted in operations per second:

```bash
stash io.nvme0n1.read
stash io.nvme0n1.write
stash io.nvme0n1.ops
```

## 51. Disk reads as pitch

Rate sources do not have a fixed natural maximum, so I/O mappings need an
explicit range chosen for the machine and workload:

```bash
stash io.nvme0n1.read \
  --range 0..1G \
  -w sine \
  -m freq=70..2k/log~100ms
```

Values below or above the input range clamp to the ends of the pitch mapping.

## 52. Disk writes open a filter

```bash
stash io.nvme0n1.write \
  --range 0..1G \
  -w saw \
  -m freq=55..440/log~100ms \
  -f lp:300,.8 \
  -m filter.cutoff=300..8k/log~150ms
```

The same write-throughput control drives both oscillator frequency and
low-pass cutoff.

## 53. Disk operation bursts as notes

```bash
stash io.nvme0n1.ops \
  -t rise:1k \
  -n C3 \
  -d 80ms \
  -w noise \
  -f lp:1k
```

This emits one short noise burst when disk activity crosses upward through
1,000 operations per second. Trigger thresholds use source values directly,
so this example does not need `--range`.

## 54. Inspect every hardware source family

CPU sources include aggregate, individual-core, vector, and optional hardware
metrics:

```bash
stash cpu.core.0.usage
stash cpu.core.0.freq
stash cpu.cores.freq
stash cpu.power
```

GPU metrics are hardware-dependent and remain discoverable as unavailable
when the local driver cannot provide them:

```bash
stash gpu.usage
stash gpu.freq
stash gpu.temp
stash gpu.power
stash gpu.vram
```

Network interfaces expose byte and packet rates in both directions:

```bash
stash net.enp4s0.rx
stash net.enp4s0.tx
stash net.enp4s0.rx.packets
stash net.enp4s0.tx.packets
```

Use `stash -l cpu`, `stash -l gpu`, and `stash -l net` to see what is present
on the current machine and replace example interface names.

## 55. Gate, hit, and step rhythm controls

Use the rhythm gate as the oscillator gate and the hit pulse to accent drive:

```bash
stash ram.used \
  -w tri \
  -m freq=80..800/log~200ms \
  -r rhythm:120:1/8:x-x-x-x- \
  -m rhythm.gate:gate=0..1 \
  -x drive:.1 \
  -m rhythm.hit:drive.amount=.05..0.8
```

Use the zero-based rhythm step to change filter resonance across the pattern:

```bash
stash ram.used \
  -w saw \
  -m freq=80..600/log \
  -r rhythm:100:1/8:x-x---x- \
  -f lp:2k,.7 \
  -m rhythm.step:filter.q=.5..8
```

The earlier rhythm examples cover `rhythm.phase` and `rhythm.velocity`; the
filter example covers `rhythm.gate`. These commands add explicit `gate`,
`rhythm.hit`, and `rhythm.step` mappings.

## 56. Modulate every effect parameter

```bash
stash ram.used \
  -w saw \
  -m freq=80..800/log~200ms \
  -f lp:2k,.7 \
  -m ram.free:filter.q=.5..8 \
  -x delay:150ms,.2,.2 \
  -m cpu.usage:delay.time=.04..0.4 \
  -m cpu.usage:delay.mix=.05..0.7 \
  -x drive:.15 \
  -m cpu.usage:drive.amount=.05..0.7
```

Along with the earlier `filter.cutoff` and `delay.feedback` examples, this
covers every modulatable filter, delay, and drive parameter. Unindexed effect
targets bind to the most recently declared matching effect.

## 57. Capability coverage index

| Capability | Examples |
| --- | --- |
| Source families: CPU, GPU, RAM, network, disk I/O, stdin | 1, 33-39, 45-54 |
| Scalar, individual-core, and vector telemetry | 1-2, 12-16, 54 |
| Notes, note arrays, scales, modes, rhythms | 3, 12-21 |
| Waveforms: sine, square, saw, triangle, noise | 4, 7-8, 47, 49 |
| Mapping curves, smoothing, ranges, and multiple controls | 4, 9, 25, 33-36, 47, 51-52 |
| Frequency, gain, pan, and gate targets | 4, 11, 32, 47, 55 |
| Above, below, rise, and fall triggers; gate duration; ADSR | 12, 15-16, 48, 53 |
| Tempo, swing, and all five rhythm controls | 17-21, 30-32, 55 |
| Low/high-pass, Q, delay, drive, ordering, and effect modulation | 22-30, 36, 49, 52, 55-56 |
| Telemetry stdout, shell pipelines, device audio, and raw PCM | 1, 4-44 |
| Listing, prefix discovery, inspection, and primitive resolution | 2-3, 45, 54 |
