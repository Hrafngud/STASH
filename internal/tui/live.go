package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/cli"
	stashruntime "github.com/zalmo/stash/internal/runtime"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
)

type applyKind string

const (
	applyStarted applyKind = "audio started"
	applyHot     applyKind = "hot update"
	applyRebuild applyKind = "graph rebuilt"
)

// liveEngine owns at most one runtime engine and serializes graph replacement.
// The editor itself remains the source of truth; this type only applies valid
// plans produced by the shared CLI planner.
type liveEngine struct {
	parent         context.Context
	registry       *source.Registry
	backend        audio.Backend
	diagnostics    io.Writer
	sampleInterval func(string) time.Duration
	rhythmInterval time.Duration
	maxDelay       time.Duration

	mu      sync.Mutex
	plan    *cli.Plan
	cancel  context.CancelFunc
	done    chan error
	updates chan []audio.Update
}

func (live *liveEngine) apply(plan cli.Plan) (applyKind, error) {
	live.mu.Lock()
	defer live.mu.Unlock()
	hadPlan := live.plan != nil
	if live.plan != nil {
		if updates, ok := hotUpdates(*live.plan, plan); ok {
			if len(updates) > 0 {
				select {
				case live.updates <- updates:
				case <-live.parent.Done():
					return "", live.parent.Err()
				}
			}
			copy := plan
			live.plan = &copy
			return applyHot, nil
		}
		live.stopLocked()
	}
	if err := live.startLocked(plan); err != nil {
		return "", err
	}
	if hadPlan {
		return applyRebuild, nil
	}
	return applyStarted, nil
}

func (live *liveEngine) startLocked(plan cli.Plan) error {
	if live.backend == nil {
		return fmt.Errorf("audio backend is nil")
	}
	workerCtx, cancel := context.WithCancel(live.parent)
	updates := make(chan []audio.Update, 16)
	done := make(chan error, 1)
	engine := stashruntime.Engine{
		Registry: live.registry, Backend: live.backend, Diagnostics: live.diagnostics,
		SampleInterval: live.sampleInterval, RhythmInterval: live.rhythmInterval,
		MaxDelay: live.maxDelay, ExternalUpdates: updates,
	}
	go func() { done <- engine.Run(workerCtx, plan) }()
	copy := plan
	live.plan, live.cancel, live.done, live.updates = &copy, cancel, done, updates
	return nil
}

func (live *liveEngine) stopLocked() {
	if live.cancel == nil {
		return
	}
	live.cancel()
	<-live.done
	live.plan, live.cancel, live.done, live.updates = nil, nil, nil, nil
}

func (live *liveEngine) close() {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.stopLocked()
}

func (live *liveEngine) pollError() error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.done == nil {
		return nil
	}
	select {
	case err := <-live.done:
		live.plan, live.cancel, live.done, live.updates = nil, nil, nil, nil
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	default:
	}
	return nil
}

func (live *liveEngine) running() bool {
	live.mu.Lock()
	defer live.mu.Unlock()
	return live.plan != nil
}

// hotUpdates recognizes edits that the backend-neutral Session contract can
// apply without replacing the graph. The comparison intentionally errs toward
// rebuilding whenever control topology or graph-time configuration changes.
func hotUpdates(oldPlan, newPlan cli.Plan) ([]audio.Update, bool) {
	a, b := oldPlan.Command, newPlan.Command
	if oldPlan.Mode != cli.ModeAudioDevice || newPlan.Mode != cli.ModeAudioDevice ||
		a.Source != b.Source || !reflect.DeepEqual(a.Modulations, b.Modulations) ||
		!reflect.DeepEqual(a.RangeOverrides, b.RangeOverrides) ||
		!reflect.DeepEqual(a.Trigger, b.Trigger) || !reflect.DeepEqual(a.Notes, b.Notes) ||
		!reflect.DeepEqual(a.Rhythm, b.Rhythm) || !reflect.DeepEqual(a.BPM, b.BPM) ||
		!reflect.DeepEqual(a.GateDuration, b.GateDuration) || !reflect.DeepEqual(a.Envelope, b.Envelope) ||
		!reflect.DeepEqual(a.Swing, b.Swing) || !reflect.DeepEqual(a.Output, b.Output) ||
		oldPlan.Sound.MasterGain != newPlan.Sound.MasterGain ||
		oldPlan.Sound.MasterGainSet != newPlan.Sound.MasterGainSet ||
		len(oldPlan.Sound.Synths) != len(newPlan.Sound.Synths) ||
		len(oldPlan.Sound.Effects) != len(newPlan.Sound.Effects) ||
		len(oldPlan.Sound.Voices) != len(newPlan.Sound.Voices) {
		return nil, false
	}

	var updates []audio.Update
	for index := range oldPlan.Sound.Synths {
		oldSynth, newSynth := oldPlan.Sound.Synths[index], newPlan.Sound.Synths[index]
		if oldSynth.Type != newSynth.Type || oldSynth.ID != newSynth.ID ||
			!reflect.DeepEqual(oldSynth.Config, newSynth.Config) ||
			!reflect.DeepEqual(oldSynth.Envelope, newSynth.Envelope) ||
			!sameKeys(oldSynth.Parameters, newSynth.Parameters) {
			return nil, false
		}
		for name, value := range newSynth.Parameters {
			if oldSynth.Parameters[name] != value {
				updates = append(updates, audio.Update{Target: sound.Target{Name: name, EffectIndex: -1, IsSynth: true, SynthIndex: index}, Value: value})
			}
		}
	}
	for index := range oldPlan.Sound.Effects {
		oldEffect, newEffect := oldPlan.Sound.Effects[index], newPlan.Sound.Effects[index]
		if oldEffect.Kind != newEffect.Kind || !reflect.DeepEqual(oldEffect.Config, newEffect.Config) {
			return nil, false
		}
		spec, ok := sound.LookupEffectSpec(newEffect.Kind)
		if !ok {
			return nil, false
		}
		for _, parameter := range spec.Parameters {
			oldValue, oldOK := oldEffect.Parameter(parameter.Name)
			newValue, newOK := newEffect.Parameter(parameter.Name)
			if !oldOK || !newOK {
				return nil, false
			}
			if oldValue != newValue {
				// Delay storage is allocated from the initial maximum. Rebuild when
				// increasing it so a hot update can never overrun that allocation.
				if newEffect.Kind == sound.EffectDelay && parameter.Name == "time" && newValue > oldValue {
					return nil, false
				}
				updates = append(updates, audio.Update{Target: sound.Target{Name: spec.Target + "." + parameter.Name, EffectIndex: index}, Value: newValue})
			}
		}
	}
	// Legacy voices are cheap but may expand after the first vector sample, so
	// only explicit synth instruments currently use the guaranteed hot path.
	if len(oldPlan.Sound.Voices) > 0 && !reflect.DeepEqual(oldPlan.Sound.Voices, newPlan.Sound.Voices) {
		return nil, false
	}
	return updates, true
}

func sameKeys(left, right map[string]float64) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}
