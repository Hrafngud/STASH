package runtime

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
)

type sequenceCollector struct {
	samples []source.Sample
	err     error
	index   int
}

func (collector *sequenceCollector) Collect(ctx context.Context) (source.Sample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if collector.index < len(collector.samples) {
		sample := collector.samples[collector.index]
		collector.index++
		return sample, nil
	}
	if collector.err != nil {
		return nil, collector.err
	}
	return nil, io.EOF
}

type blockingCollector struct {
	first source.Sample
	once  bool
}

func (collector *blockingCollector) Collect(ctx context.Context) (source.Sample, error) {
	if !collector.once {
		collector.once = true
		return collector.first, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type fakeBackend struct {
	mu       sync.Mutex
	starts   int
	config   audio.Config
	session  *fakeSession
	startErr error
	started  chan struct{}
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{session: newFakeSession(), started: make(chan struct{})}
}

func (backend *fakeBackend) Start(_ context.Context, config audio.Config) (audio.Session, error) {
	if backend.startErr != nil {
		return nil, backend.startErr
	}
	backend.mu.Lock()
	backend.starts++
	backend.config = config
	backend.config.Model = cloneSoundModel(config.Model)
	backend.mu.Unlock()
	select {
	case <-backend.started:
	default:
		close(backend.started)
	}
	return backend.session, nil
}

type fakeSession struct {
	mu      sync.Mutex
	updates []audio.Update
	done    chan struct{}
	once    sync.Once
	err     error
}

func newFakeSession() *fakeSession { return &fakeSession{done: make(chan struct{})} }

func (session *fakeSession) Update(ctx context.Context, update audio.Update) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session.mu.Lock()
	session.updates = append(session.updates, update)
	session.mu.Unlock()
	return nil
}

func (session *fakeSession) Wait() error {
	<-session.done
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

func (session *fakeSession) Close() error {
	session.once.Do(func() { close(session.done) })
	return session.Wait()
}

func (session *fakeSession) fail(err error) {
	session.mu.Lock()
	session.err = err
	session.mu.Unlock()
	session.once.Do(func() { close(session.done) })
}

func (session *fakeSession) snapshot() []audio.Update {
	session.mu.Lock()
	defer session.mu.Unlock()
	return append([]audio.Update(nil), session.updates...)
}

func cloneSoundModel(model sound.Model) sound.Model {
	return sound.Model{
		Voices:  append([]sound.Voice(nil), model.Voices...),
		Effects: append([]sound.Effect(nil), model.Effects...),
	}
}

func registerCollector(t *testing.T, registry *source.Registry, info source.Info, collector source.Collector) {
	t.Helper()
	if err := registry.RegisterAvailable(info, func(context.Context) (source.Collector, error) {
		return collector, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func rangedInfo(name string, kind source.Kind) source.Info {
	minimum, maximum := 0.0, 100.0
	return source.Info{Name: name, Kind: kind, Unit: "%", NaturalMin: &minimum, NaturalMax: &maximum}
}

func buildPlan(t *testing.T, registry *source.Registry, args ...string) cli.Plan {
	t.Helper()
	command, err := cli.Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func immediateEngine(registry *source.Registry, backend audio.Backend) *Engine {
	return &Engine{
		Registry: registry,
		Backend:  backend,
		SampleInterval: func(string) time.Duration {
			return 0
		},
	}
}

func TestScalarMappingSmoothsExplicitDeltasWithoutRestartingBackend(t *testing.T) {
	t.Parallel()
	origin := time.Unix(10, 0)
	collector := &sequenceCollector{samples: []source.Sample{
		source.ScalarSample{Value: 0, Time: origin},
		source.ScalarSample{Value: 100, Time: origin.Add(100 * time.Millisecond)},
	}}
	registry := source.NewRegistry()
	registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), collector)
	backend := newFakeBackend()
	plan := buildPlan(t, registry, "cpu.usage", "-m", "freq=100..1100~100ms")

	if err := immediateEngine(registry, backend).Run(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if backend.starts != 1 {
		t.Fatalf("backend starts = %d, want 1 persistent session", backend.starts)
	}
	if got := backend.config.Model.Voices[0].Frequency; got != 440 {
		t.Fatalf("initial smoothed frequency = %v, want 440", got)
	}
	updates := backend.session.snapshot()
	if len(updates) != 1 || updates[0].Target.Name != "freq" || updates[0].VoiceIndex != 0 {
		t.Fatalf("updates = %#v, want one frequency channel update", updates)
	}
	want := 440 + (1100-440)*(1-math.Exp(-1))
	if math.Abs(updates[0].Value-want) > 1e-9 {
		t.Fatalf("smoothed frequency = %v, want %v", updates[0].Value, want)
	}
}

func TestVectorTriggerBuildsPolyphonicNotesAndIndependentGates(t *testing.T) {
	t.Parallel()
	origin := time.Unix(20, 0)
	collector := &sequenceCollector{samples: []source.Sample{
		source.VectorSample{Values: []float64{0, 0}, Time: origin},
		source.VectorSample{Values: []float64{60, 70}, Time: origin.Add(50 * time.Millisecond)},
	}}
	registry := source.NewRegistry()
	registerCollector(t, registry, rangedInfo("cpu.cores.usage", source.KindVector), collector)
	backend := newFakeBackend()
	plan := buildPlan(t, registry, "cpu.cores.usage", "-t", "rise:50", "-n", "C4,E4", "-d", "100ms")

	if err := immediateEngine(registry, backend).Run(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	voices := backend.config.Model.Voices
	if len(voices) != 2 || voices[0].Frequency >= voices[1].Frequency || voices[0].Gate != 0 || voices[1].Gate != 0 {
		t.Fatalf("preflight voices = %#v", voices)
	}
	updates := backend.session.snapshot()
	seen := [2]bool{}
	for _, update := range updates {
		if update.Target.Name == "gate" && update.Value == 1 && update.VoiceIndex >= 0 && update.VoiceIndex < len(seen) {
			seen[update.VoiceIndex] = true
		}
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("gate updates = %#v, want independent voice 0 and 1 events", updates)
	}
}

func TestVectorNoteCountFailsBeforeBackendStart(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	registerCollector(t, registry, rangedInfo("cpu.cores.usage", source.KindVector), &sequenceCollector{samples: []source.Sample{
		source.VectorSample{Values: []float64{1, 2}, Time: time.Unix(30, 0)},
	}})
	backend := newFakeBackend()
	plan := buildPlan(t, registry, "cpu.cores.usage", "-t", "above:0", "-n", "C4")

	err := immediateEngine(registry, backend).Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "2 vector values require at least 2 notes; got 1") {
		t.Fatalf("Run() error = %v", err)
	}
	if backend.starts != 0 {
		t.Fatalf("backend started %d times before vector-note validation", backend.starts)
	}
}

func TestMultipleTelemetryControlsApplyNaturalRanges(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	primary := &blockingCollector{first: source.ScalarSample{Value: 50, Time: time.Unix(40, 0)}}
	secondary := &sequenceCollector{samples: []source.Sample{
		source.ScalarSample{Value: 25, Time: time.Unix(40, 0)},
	}, err: errors.New("sensor disappeared")}
	registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), primary)
	registerCollector(t, registry, rangedInfo("cpu.temp", source.KindScalar), secondary)
	backend := newFakeBackend()
	plan := buildPlan(t, registry,
		"cpu.usage", "-f", "lp:2k",
		"-m", "gain=0..1", "-m", "cpu.temp:pan=-1..1",
		"-m", "cpu.temp:filter.cutoff=100..1100",
	)

	err := immediateEngine(registry, backend).Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "sensor disappeared") {
		t.Fatalf("Run() error = %v", err)
	}
	voice := backend.config.Model.Voices[0]
	if voice.Gain != .5 || voice.Pan != -.5 {
		t.Fatalf("initial multi-control voice = %#v", voice)
	}
	if got := backend.config.Model.Effects[0].Cutoff; got != 350 {
		t.Fatalf("initial effect cutoff = %v, want 350", got)
	}
}

func TestRangeOverrideReplacesNaturalNormalization(t *testing.T) {
	t.Parallel()
	minimum, maximum := 0.0, 100.0
	info := source.Info{Name: "sensor", Kind: source.KindScalar, Unit: "u", NaturalMin: &minimum, NaturalMax: &maximum}
	registry := source.NewRegistry()
	registerCollector(t, registry, info, &sequenceCollector{samples: []source.Sample{
		source.ScalarSample{Value: 1, Time: time.Unix(45, 0)},
	}})
	backend := newFakeBackend()
	plan := buildPlan(t, registry, "sensor", "--range", "0..2", "-m", "gain=0..1")
	if err := immediateEngine(registry, backend).Run(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if got := backend.config.Model.Voices[0].Gain; got != .5 {
		t.Fatalf("override-mapped gain = %v, want .5", got)
	}
}

type manualTimer struct {
	deadline time.Time
	channel  chan time.Time
}

type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) After(delay time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	timer := &manualTimer{deadline: clock.now.Add(delay), channel: make(chan time.Time, 1)}
	clock.timers = append(clock.timers, timer)
	return timer.channel
}

func (clock *manualClock) advance(delay time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(delay)
	remaining := clock.timers[:0]
	for _, timer := range clock.timers {
		if !timer.deadline.After(clock.now) {
			timer.channel <- timer.deadline
		} else {
			remaining = append(remaining, timer)
		}
	}
	clock.timers = remaining
	clock.mu.Unlock()
}

func (clock *manualClock) waitForTimers(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		clock.mu.Lock()
		count := len(clock.timers)
		clock.mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for a manual clock timer")
}

func TestPreflightWaitsForPolledSourceCadence(t *testing.T) {
	origin := time.Unix(49, 0)
	clock := &manualClock{now: origin}
	registry := source.NewRegistry()
	registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), &blockingCollector{
		first: source.ScalarSample{Value: 50, Time: origin.Add(10 * time.Millisecond)},
	})
	backend := newFakeBackend()
	plan := buildPlan(t, registry, "cpu.usage", "-w", "sine")
	engine := &Engine{
		Registry: registry,
		Backend:  backend,
		Clock:    clock,
		SampleInterval: func(string) time.Duration {
			return 10 * time.Millisecond
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- engine.Run(ctx, plan) }()

	clock.waitForTimers(t)
	backend.mu.Lock()
	startsBeforeTick := backend.starts
	backend.mu.Unlock()
	if startsBeforeTick != 0 {
		cancel()
		t.Fatalf("backend starts before first source tick = %d, want 0", startsBeforeTick)
	}
	clock.advance(10 * time.Millisecond)
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("backend did not start after first source tick")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRhythmControlsUseInjectedClockAndCancellation(t *testing.T) {
	t.Parallel()
	origin := time.Unix(50, 0)
	clock := &manualClock{now: origin}
	registry := source.NewRegistry()
	registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), &blockingCollector{
		first: source.ScalarSample{Value: 50, Time: origin},
	})
	backend := newFakeBackend()
	plan := buildPlan(t, registry,
		"cpu.usage", "-r", "rhythm:120:1/8:x-", "-m", "rhythm.phase:pan=-1..1",
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	engine := immediateEngine(registry, backend)
	engine.Clock = clock
	engine.RhythmInterval = 5 * time.Millisecond
	go func() { result <- engine.Run(ctx, plan) }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("backend did not start")
	}
	if voice := backend.config.Model.Voices[0]; voice.Gate != 1 || voice.Pan != -1 {
		t.Fatalf("initial rhythm voice = %#v", voice)
	}
	clock.waitForTimers(t)
	clock.advance(5 * time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for {
		updates := backend.session.snapshot()
		for _, update := range updates {
			if update.Target.Name == "pan" && update.Value > -1 {
				cancel()
				err := <-result
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Run() error = %v, want context.Canceled", err)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("rhythm phase update not observed: %#v", updates)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRendererAndSourceFailuresPropagate(t *testing.T) {
	t.Parallel()
	t.Run("renderer", func(t *testing.T) {
		registry := source.NewRegistry()
		registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), &blockingCollector{
			first: source.ScalarSample{Value: 1, Time: time.Unix(60, 0)},
		})
		backend := newFakeBackend()
		plan := buildPlan(t, registry, "cpu.usage", "-w", "sine")
		result := make(chan error, 1)
		go func() { result <- immediateEngine(registry, backend).Run(context.Background(), plan) }()
		<-backend.started
		renderErr := errors.New("renderer exploded")
		backend.session.fail(renderErr)
		if err := <-result; !errors.Is(err, renderErr) {
			t.Fatalf("Run() error = %v, want renderer error", err)
		}
	})

	t.Run("source", func(t *testing.T) {
		sourceErr := errors.New("source disappeared")
		registry := source.NewRegistry()
		registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), &sequenceCollector{
			samples: []source.Sample{source.ScalarSample{Value: 1, Time: time.Unix(70, 0)}},
			err:     sourceErr,
		})
		backend := newFakeBackend()
		plan := buildPlan(t, registry, "cpu.usage", "-w", "sine")
		if err := immediateEngine(registry, backend).Run(context.Background(), plan); !errors.Is(err, sourceErr) {
			t.Fatalf("Run() error = %v, want source error", err)
		}
	})
}

func TestRunValidationAndRawPCMConfiguration(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	registerCollector(t, registry, rangedInfo("cpu.usage", source.KindScalar), &sequenceCollector{samples: []source.Sample{
		source.ScalarSample{Value: 1, Time: time.Unix(80, 0)},
	}})
	backend := newFakeBackend()
	plan := buildPlan(t, registry, "cpu.usage", "-o", "-")
	engine := immediateEngine(registry, backend)
	engine.PCM = io.Discard
	if err := engine.Run(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if backend.config.Output != audio.OutputRawPCM || backend.config.PCM != io.Discard {
		t.Fatalf("raw config = %#v", backend.config)
	}
	if err := (*Engine)(nil).Run(context.Background(), plan); err == nil {
		t.Fatal("nil engine unexpectedly succeeded")
	}
}
