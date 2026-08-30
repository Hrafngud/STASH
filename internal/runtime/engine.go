// Package runtime coordinates telemetry, controls, rhythm, and a persistent
// audio backend. Collection never occurs in an audio render callback.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
	"github.com/zalmo/stash/internal/unit"
)

const defaultRhythmInterval = 5 * time.Millisecond

// Engine owns one audio-mode execution. SampleInterval may override source
// cadence per name; nil uses 20 Hz for polled sources and producer cadence for
// stdin. RhythmInterval zero selects the low-latency default.
type Engine struct {
	Registry       *source.Registry
	Backend        audio.Backend
	Clock          Clock
	SampleInterval func(string) time.Duration
	RhythmInterval time.Duration
	PCM            io.Writer
	Diagnostics    io.Writer
	MaxDelay       time.Duration
}

// Run preflights all controls and the first primary sample before starting one
// persistent audio session, then runs independent collection and rhythm loops.
func (engine *Engine) Run(ctx context.Context, plan cli.Plan) error {
	if ctx == nil {
		return fmt.Errorf("run audio: context is nil")
	}
	if engine == nil {
		return fmt.Errorf("run audio: engine is nil")
	}
	if engine.Registry == nil {
		return fmt.Errorf("run audio: source registry is nil")
	}
	if engine.Backend == nil {
		return fmt.Errorf("run audio: backend is nil")
	}
	if engine.MaxDelay < 0 {
		return fmt.Errorf("run audio: maximum delay must be non-negative")
	}
	if plan.Mode != cli.ModeAudioDevice && plan.Mode != cli.ModeRawPCM {
		return fmt.Errorf("run audio: plan mode %q is not an audio mode", plan.Mode)
	}
	clock := engine.Clock
	if clock == nil {
		clock = wallClock{}
	}

	prepared, order, err := engine.prepareSources(ctx, clock, plan)
	if err != nil {
		return fmt.Errorf("run audio: %w", err)
	}
	primary := prepared[plan.Command.Source]
	model, err := prepareModel(plan, primary.values)
	if err != nil {
		return fmt.Errorf("run audio: %w", err)
	}
	state, err := newControlState(plan, model, prepared, clock)
	if err != nil {
		return fmt.Errorf("run audio: %w", err)
	}
	if err := state.applyInitial(prepared, order); err != nil {
		return fmt.Errorf("run audio: initialize controls: %w", err)
	}

	maxDelay, err := engine.resolveMaxDelay(plan)
	if err != nil {
		return fmt.Errorf("run audio: %w", err)
	}
	output := audio.OutputDevice
	if plan.Mode == cli.ModeRawPCM {
		output = audio.OutputRawPCM
	}
	intervals := make(map[string]time.Duration, len(order))
	for _, name := range order {
		interval, intervalErr := engine.sourceInterval(name)
		if intervalErr != nil {
			return fmt.Errorf("run audio: %w", intervalErr)
		}
		intervals[name] = interval
	}
	rhythmInterval := engine.RhythmInterval
	if rhythmInterval == 0 {
		rhythmInterval = defaultRhythmInterval
	}
	if rhythmInterval < 0 {
		return fmt.Errorf("run audio: rhythm interval must be non-negative")
	}

	session, err := engine.Backend.Start(ctx, audio.Config{
		Model:       state.model,
		Output:      output,
		PCM:         engine.PCM,
		Diagnostics: engine.Diagnostics,
		MaxDelay:    maxDelay,
	})
	if err != nil {
		return fmt.Errorf("run audio: start backend: %w", err)
	}
	if session == nil {
		return fmt.Errorf("run audio: backend returned a nil session")
	}
	state.session = session

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	state.updateContext = workerCtx
	events := make(chan runtimeEvent, 64)
	for _, name := range order {
		item := prepared[name]
		go collectLoop(workerCtx, clock, item, intervals[name], events)
	}
	if state.rhythmClock != nil {
		go rhythmLoop(workerCtx, clock, rhythmInterval, events)
	}
	go func() { sendEvent(workerCtx, events, runtimeEvent{kind: eventRenderer, err: session.Wait()}) }()
	state.scheduleInitialGates(workerCtx, clock, events)

	runErr := state.run(workerCtx, clock, events)
	cancelWorkers()
	closeErr := session.Close()
	if runErr != nil {
		return fmt.Errorf("run audio: %w", runErr)
	}
	if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
		return fmt.Errorf("run audio: close backend: %w", closeErr)
	}
	return nil
}

type preparedSource struct {
	name      string
	entry     source.Entry
	collector source.Collector
	values    []float64
	at        time.Time
	length    int
}

func (engine *Engine) prepareSources(ctx context.Context, clock Clock, plan cli.Plan) (map[string]*preparedSource, []string, error) {
	names := []string{plan.Command.Source}
	seen := map[string]bool{plan.Command.Source: true}
	for _, modulation := range plan.Command.Modulations {
		name := modulation.Control
		if name == "" {
			name = plan.Command.Source
		}
		if isRhythmControl(name) || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}

	prepared := make(map[string]*preparedSource, len(names))
	for _, name := range names {
		entry, ok := engine.Registry.Lookup(name)
		if !ok {
			return nil, nil, fmt.Errorf("unknown control source: %s", name)
		}
		collector, err := engine.Registry.NewCollector(ctx, name)
		if err != nil {
			return nil, nil, fmt.Errorf("open control %s: %w", name, err)
		}
		interval, err := engine.sourceInterval(name)
		if err != nil {
			return nil, nil, err
		}
		if interval > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-clock.After(interval):
			}
		}
		sample, err := collector.Collect(ctx)
		if errors.Is(err, io.EOF) {
			return nil, nil, fmt.Errorf("control %s ended before its first sample", name)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("collect initial control %s: %w", name, err)
		}
		values, at, err := sampleValues(entry.Info, sample)
		if err != nil {
			return nil, nil, fmt.Errorf("collect initial control %s: %w", name, err)
		}
		prepared[name] = &preparedSource{
			name: name, entry: entry, collector: collector,
			values: values, at: at, length: len(values),
		}
	}
	return prepared, names, nil
}

func prepareModel(plan cli.Plan, primaryValues []float64) (sound.Model, error) {
	if len(primaryValues) == 0 {
		return sound.Model{}, fmt.Errorf("primary source sample has no values")
	}
	if len(plan.Sound.Voices) == 0 {
		return sound.Model{}, fmt.Errorf("plan has no sound voice")
	}
	model := sound.Model{
		Effects: append([]sound.Effect(nil), plan.Sound.Effects...),
	}
	base := plan.Sound.Voices[0]
	voiceCount := 1
	if plan.SourceEntry.Info.Kind == source.KindVector {
		voiceCount = len(primaryValues)
	}
	if voiceCount < 1 {
		return sound.Model{}, fmt.Errorf("primary vector sample has no values")
	}
	if plan.SourceEntry.Info.Kind == source.KindVector && len(plan.Command.Notes) > 0 {
		if err := primitive.ValidateVectorNotes(voiceCount, len(plan.Command.Notes)); err != nil {
			return sound.Model{}, err
		}
	}
	model.Voices = make([]sound.Voice, voiceCount)
	for index := range model.Voices {
		model.Voices[index] = base
		if len(plan.Command.Notes) > 0 {
			noteIndex := index
			if plan.SourceEntry.Info.Kind == source.KindScalar {
				noteIndex = 0
			}
			model.Voices[index].Frequency = plan.Command.Notes[noteIndex].Frequency()
		}
		if plan.Command.Trigger != nil || plan.Command.Rhythm != nil {
			model.Voices[index].Gate = 0
		}
	}
	if err := model.Validate(); err != nil {
		return sound.Model{}, fmt.Errorf("invalid prepared sound model: %w", err)
	}
	return model, nil
}

func (engine *Engine) sourceInterval(name string) (time.Duration, error) {
	interval := source.DefaultSampleInterval
	if name == "-" {
		interval = 0
	}
	if engine.SampleInterval != nil {
		interval = engine.SampleInterval(name)
	}
	if interval < 0 {
		return 0, fmt.Errorf("source %s interval must be non-negative", name)
	}
	return interval, nil
}

func (engine *Engine) resolveMaxDelay(plan cli.Plan) (time.Duration, error) {
	required := time.Duration(0)
	for _, effect := range plan.Sound.Effects {
		if effect.Kind == sound.EffectDelay && effect.DelayTime > required {
			required = effect.DelayTime
		}
	}
	for index, target := range plan.SoundTargets {
		if target.Name != "delay.time" {
			continue
		}
		seconds := plan.Command.Modulations[index].Mapping.Output.Max
		if seconds > float64(math.MaxInt64)/float64(time.Second) {
			return 0, fmt.Errorf("delay mapping maximum is too large")
		}
		delay := time.Duration(math.Ceil(seconds * float64(time.Second)))
		if delay > required {
			required = delay
		}
	}
	if engine.MaxDelay > 0 {
		if required > engine.MaxDelay {
			return 0, fmt.Errorf("required delay %s exceeds configured maximum %s", required, engine.MaxDelay)
		}
		return engine.MaxDelay, nil
	}
	return required, nil
}

func sampleValues(info source.Info, sample source.Sample) ([]float64, time.Time, error) {
	var values []float64
	var at time.Time
	switch typed := sample.(type) {
	case source.ScalarSample:
		values, at = []float64{typed.Value}, typed.Time
	case *source.ScalarSample:
		if typed == nil {
			return nil, time.Time{}, fmt.Errorf("sample is nil")
		}
		values, at = []float64{typed.Value}, typed.Time
	case source.VectorSample:
		values, at = append([]float64(nil), typed.Values...), typed.Time
	case *source.VectorSample:
		if typed == nil {
			return nil, time.Time{}, fmt.Errorf("sample is nil")
		}
		values, at = append([]float64(nil), typed.Values...), typed.Time
	case nil:
		return nil, time.Time{}, fmt.Errorf("sample is nil")
	default:
		return nil, time.Time{}, fmt.Errorf("unsupported sample type %T", sample)
	}
	if sample.SampleKind() != info.Kind {
		return nil, time.Time{}, fmt.Errorf("sample kind %s does not match registered kind %s", sample.SampleKind(), info.Kind)
	}
	if len(values) == 0 {
		return nil, time.Time{}, fmt.Errorf("sample has no values")
	}
	for index, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, time.Time{}, fmt.Errorf("sample value %d is not finite", index)
		}
	}
	return values, at, nil
}

type eventKind uint8

const (
	eventSource eventKind = iota
	eventRhythm
	eventGateOff
	eventRenderer
)

type runtimeEvent struct {
	kind       eventKind
	name       string
	values     []float64
	at         time.Time
	err        error
	voiceIndex int
	generation uint64
}

func collectLoop(ctx context.Context, clock Clock, item *preparedSource, interval time.Duration, events chan<- runtimeEvent) {
	for {
		if interval > 0 {
			select {
			case <-ctx.Done():
				return
			case <-clock.After(interval):
			}
		}
		sample, err := item.collector.Collect(ctx)
		if err != nil {
			sendEvent(ctx, events, runtimeEvent{kind: eventSource, name: item.name, err: err})
			return
		}
		values, at, valueErr := sampleValues(item.entry.Info, sample)
		if valueErr == nil && len(values) != item.length {
			valueErr = fmt.Errorf("sample width changed from %d to %d", item.length, len(values))
		}
		if valueErr != nil {
			sendEvent(ctx, events, runtimeEvent{kind: eventSource, name: item.name, err: valueErr})
			return
		}
		if !sendEvent(ctx, events, runtimeEvent{kind: eventSource, name: item.name, values: values, at: at}) {
			return
		}
		if interval == 0 {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}
}

func rhythmLoop(ctx context.Context, clock Clock, interval time.Duration, events chan<- runtimeEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-clock.After(interval):
			if !sendEvent(ctx, events, runtimeEvent{kind: eventRhythm, at: at}) {
				return
			}
		}
	}
}

func sendEvent(ctx context.Context, events chan<- runtimeEvent, event runtimeEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
}

func isRhythmControl(name string) bool {
	return len(name) > len("rhythm.") && name[:len("rhythm.")] == "rhythm."
}

func naturalRange(info source.Info) (unit.Range, error) {
	if info.NaturalMin == nil || info.NaturalMax == nil {
		return unit.Range{}, fmt.Errorf("control %s has no natural range", info.Name)
	}
	return unit.Range{Min: *info.NaturalMin, Max: *info.NaturalMax}, nil
}
