package app_test

import (
	"bytes"
	"context"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/app"
	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
	stdinsource "github.com/zalmo/stash/internal/source/stdin"
)

type scriptedCollector struct {
	samples []source.Sample
	index   int
}

func (collector *scriptedCollector) Collect(ctx context.Context) (source.Sample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if collector.index == len(collector.samples) {
		return nil, io.EOF
	}
	sample := collector.samples[collector.index]
	collector.index++
	return sample, nil
}

type recordingBackend struct {
	mu       sync.Mutex
	configs  []audio.Config
	updates  []audio.Update
	rawBytes []byte
}

func (backend *recordingBackend) Start(_ context.Context, config audio.Config) (audio.Session, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	backend.mu.Lock()
	backend.configs = append(backend.configs, config)
	backend.mu.Unlock()
	if config.Output == audio.OutputRawPCM && len(backend.rawBytes) > 0 {
		if _, err := config.PCM.Write(backend.rawBytes); err != nil {
			return nil, err
		}
	}
	return &recordingSession{backend: backend, done: make(chan struct{})}, nil
}

func (backend *recordingBackend) config(t *testing.T) audio.Config {
	t.Helper()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.configs) != 1 {
		t.Fatalf("backend starts = %d, want 1", len(backend.configs))
	}
	return backend.configs[0]
}

type recordingSession struct {
	backend *recordingBackend
	done    chan struct{}
	once    sync.Once
}

func (session *recordingSession) Update(ctx context.Context, update audio.Update) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session.backend.mu.Lock()
	session.backend.updates = append(session.backend.updates, update)
	session.backend.mu.Unlock()
	return nil
}

func (session *recordingSession) Wait() error {
	<-session.done
	return nil
}

func (session *recordingSession) Close() error {
	session.once.Do(func() { close(session.done) })
	return nil
}

func TestDefinitionOfDoneCommandsEndToEnd(t *testing.T) {
	t.Run("CPU telemetry", func(t *testing.T) {
		registry := source.NewRegistry()
		registerScript(t, registry, "cpu.usage", source.KindScalar, []source.Sample{
			scalar(12.5, 0), scalar(80, 1),
		})
		runner := finiteRunner(registry, nil)
		var stdout, stderr bytes.Buffer
		if err := runner.Run(context.Background(), []string{"cpu.usage"}, &stdout, &stderr); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got, want := stdout.String(), "12.5\n80\n"; got != want {
			t.Fatalf("stdout = %q, want %q", got, want)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("continuous sine mapping", func(t *testing.T) {
		registry := source.NewRegistry()
		registerScript(t, registry, "cpu.usage", source.KindScalar, []source.Sample{
			scalar(10, 0), scalar(90, 1),
		})
		backend := &recordingBackend{}
		runner := finiteRunner(registry, backend)
		assertAudioCommand(t, runner, backend, []string{
			"cpu.usage", "-w", "sine", "-m", "freq=80..2k/exp~150ms",
		}, func(config audio.Config) {
			if got := config.Model.Voices[0].Waveform; got != sound.WaveSine {
				t.Errorf("waveform = %q, want sine", got)
			}
		})
	})

	t.Run("vector-triggered mode notes", func(t *testing.T) {
		registry := source.NewRegistry()
		values := make([]float64, 12)
		values[3] = 99
		registerScript(t, registry, "cpu.cores.usage", source.KindVector, []source.Sample{
			vector(values, 0),
		})
		backend := &recordingBackend{}
		runner := finiteRunner(registry, backend)
		assertAudioCommand(t, runner, backend, []string{
			"cpu.cores.usage", "-t", "above:95", "-n", "mode:E3:phrygian:12", "-d", "150ms",
		}, func(config audio.Config) {
			if got := len(config.Model.Voices); got != 12 {
				t.Fatalf("voices = %d, want 12", got)
			}
			if config.Model.Voices[3].Gate != 1 {
				t.Errorf("triggered voice gate = %v, want 1", config.Model.Voices[3].Gate)
			}
			if math.Abs(config.Model.Voices[0].Frequency-164.81377845643496) > 1e-9 {
				t.Errorf("first note frequency = %v, want E3", config.Model.Voices[0].Frequency)
			}
		})
	})

	t.Run("ordered filter and drive", func(t *testing.T) {
		registry := source.NewRegistry()
		registerScript(t, registry, "cpu.usage", source.KindScalar, []source.Sample{scalar(50, 0)})
		backend := &recordingBackend{}
		runner := finiteRunner(registry, backend)
		assertAudioCommand(t, runner, backend, []string{
			"cpu.usage", "-w", "saw", "-m", "freq=80..1k/exp~100ms", "-f", "lp:3k", "-x", "drive:.2",
		}, func(config audio.Config) {
			if got := config.Model.Voices[0].Waveform; got != sound.WaveSaw {
				t.Errorf("waveform = %q, want saw", got)
			}
			if got := len(config.Model.Effects); got != 2 {
				t.Fatalf("effects = %d, want 2", got)
			}
			if config.Model.Effects[0].Kind != sound.EffectLowPass || config.Model.Effects[1].Kind != sound.EffectDrive {
				t.Errorf("effect order = %v, %v", config.Model.Effects[0].Kind, config.Model.Effects[1].Kind)
			}
		})
	})

	t.Run("stdin source audio", func(t *testing.T) {
		registry := source.NewRegistry()
		if err := stdinsource.Register(registry, func(context.Context) (io.Reader, error) {
			return strings.NewReader("0\n.5\n1\n"), nil
		}, func() time.Time { return sampleTime(0) }); err != nil {
			t.Fatalf("register stdin: %v", err)
		}
		backend := &recordingBackend{}
		runner := finiteRunner(registry, backend)
		assertAudioCommand(t, runner, backend, []string{
			"-", "--range", "0..1", "-w", "sine", "-m", "freq=100..2k",
		}, func(config audio.Config) {
			if config.Output != audio.OutputDevice {
				t.Errorf("output = %v, want device", config.Output)
			}
		})
	})
}

func TestDiscoveryAndRawPCMKeepOutputDomainsSeparate(t *testing.T) {
	registry := source.NewRegistry()
	registerScript(t, registry, "cpu.usage", source.KindScalar, []source.Sample{scalar(42, 0)})

	t.Run("primitive", func(t *testing.T) {
		runner := finiteRunner(registry, nil)
		var stdout, stderr bytes.Buffer
		if err := runner.Run(context.Background(), []string{"-p", "C4"}, &stdout, &stderr); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "C4") || stderr.Len() != 0 {
			t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
		}
	})

	t.Run("raw PCM", func(t *testing.T) {
		backend := &recordingBackend{rawBytes: []byte{0, 0, 128, 63}}
		runner := finiteRunner(registry, backend)
		var stdout, stderr bytes.Buffer
		if err := runner.Run(context.Background(), []string{"cpu.usage", "-m", "freq=80..2k", "-o", "-"}, &stdout, &stderr); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if !bytes.Equal(stdout.Bytes(), backend.rawBytes) {
			t.Fatalf("stdout = %v, want PCM %v", stdout.Bytes(), backend.rawBytes)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
		if config := backend.config(t); config.Output != audio.OutputRawPCM {
			t.Errorf("output = %v, want raw PCM", config.Output)
		}
	})
}

func assertAudioCommand(t *testing.T, runner *app.Runner, backend *recordingBackend, args []string, inspect func(audio.Config)) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := runner.Run(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout has %d bytes in device mode", stdout.Len())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	config := backend.config(t)
	if config.Output != audio.OutputDevice {
		t.Fatalf("output = %v, want device", config.Output)
	}
	inspect(config)
}

func finiteRunner(registry *source.Registry, backend audio.Backend) *app.Runner {
	return &app.Runner{
		Registry: registry,
		Backend:  backend,
		SampleInterval: func(string) time.Duration {
			return 0
		},
	}
}

func registerScript(t *testing.T, registry *source.Registry, name string, kind source.Kind, samples []source.Sample) {
	t.Helper()
	minimum, maximum := 0.0, 100.0
	info := source.Info{Name: name, Kind: kind, Unit: "%", NaturalMin: &minimum, NaturalMax: &maximum}
	if err := registry.RegisterAvailable(info, func(context.Context) (source.Collector, error) {
		return &scriptedCollector{samples: append([]source.Sample(nil), samples...)}, nil
	}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func scalar(value float64, tick int) source.Sample {
	return source.ScalarSample{Value: value, Time: sampleTime(tick)}
}

func vector(values []float64, tick int) source.Sample {
	return source.VectorSample{Values: append([]float64(nil), values...), Time: sampleTime(tick)}
}

func sampleTime(tick int) time.Time {
	return time.Unix(1_700_000_000, int64(tick)*int64(time.Millisecond))
}
