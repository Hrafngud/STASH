package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/control"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
	"github.com/zalmo/stash/internal/unit"
)

type controlState struct {
	plan          cli.Plan
	model         sound.Model
	session       audio.Session
	updateContext context.Context
	bindings      []*mappingBinding
	trigger       *triggerBinding
	rhythmClock   *primitive.RhythmClock
	rhythmOrigin  time.Time
	gateValues    []float64
	gateGen       []uint64
}

type mappingBinding struct {
	control string
	target  sound.Target
	mapping control.Mapping
	input   unit.Range
	mappers []*control.Mapper
	last    []time.Time
	hasLast []bool
}

type triggerBinding struct {
	scalar *control.TriggerState
	vector *control.VectorTriggerState
}

func newControlState(plan cli.Plan, model sound.Model, prepared map[string]*preparedSource, clock Clock) (*controlState, error) {
	if len(plan.Command.Modulations) != len(plan.SoundTargets) {
		return nil, fmt.Errorf("plan has %d modulations but %d resolved targets", len(plan.Command.Modulations), len(plan.SoundTargets))
	}
	state := &controlState{
		plan:       plan,
		model:      model,
		gateValues: make([]float64, len(model.Voices)),
		gateGen:    make([]uint64, len(model.Voices)),
	}
	for index := range model.Voices {
		state.gateValues[index] = model.Voices[index].Gate
	}

	for index, modulation := range plan.Command.Modulations {
		name := modulation.Control
		if name == "" {
			name = plan.Command.Source
		}
		input, err := state.inputRange(name, prepared)
		if err != nil {
			return nil, fmt.Errorf("modulation control %s: %w", name, err)
		}
		state.bindings = append(state.bindings, &mappingBinding{
			control: name, target: plan.SoundTargets[index], mapping: modulation.Mapping, input: input,
		})
	}
	if plan.Command.Trigger != nil {
		binding := &triggerBinding{}
		var err error
		if plan.SourceEntry.Info.Kind == source.KindVector {
			binding.vector, err = control.NewVectorTriggerState(*plan.Command.Trigger)
		} else {
			binding.scalar, err = control.NewTriggerState(*plan.Command.Trigger)
		}
		if err != nil {
			return nil, err
		}
		state.trigger = binding
	}
	if plan.Command.Rhythm != nil {
		origin := clock.Now()
		state.rhythmOrigin = origin
		rhythmClock, err := primitive.NewRhythmClock(*plan.Command.Rhythm, plan.Command.BPM, plan.Swing, origin)
		if err != nil {
			return nil, err
		}
		state.rhythmClock = rhythmClock
	}
	return state, nil
}

func (state *controlState) inputRange(name string, prepared map[string]*preparedSource) (unit.Range, error) {
	if override := state.plan.Command.RangeOverride; override != nil {
		overrideName := override.Control
		if overrideName == "" {
			overrideName = state.plan.Command.Source
		}
		if overrideName == name {
			return override.Range, nil
		}
	}
	if isRhythmControl(name) {
		switch name {
		case "rhythm.gate", "rhythm.hit", "rhythm.velocity", "rhythm.phase":
			return unit.Range{Min: 0, Max: 1}, nil
		case "rhythm.step":
			maximum := float64(state.plan.Command.Rhythm.StepCount() - 1)
			if maximum < 1 {
				maximum = 1
			}
			return unit.Range{Min: 0, Max: maximum}, nil
		default:
			return unit.Range{}, fmt.Errorf("unknown rhythm control %s", name)
		}
	}
	item := prepared[name]
	if item == nil {
		return unit.Range{}, fmt.Errorf("control source was not prepared")
	}
	return naturalRange(item.entry.Info)
}

func (state *controlState) applyInitial(prepared map[string]*preparedSource, order []string) error {
	for _, name := range order {
		item := prepared[name]
		if err := state.applyMappings(name, item.values, item.at); err != nil {
			return err
		}
		if name == state.plan.Command.Source && state.trigger != nil {
			active, err := state.evaluateTrigger(item.values)
			if err != nil {
				return err
			}
			for _, index := range active {
				state.gateGen[index]++
				if err := state.setGate(index, 1); err != nil {
					return err
				}
			}
		}
	}
	if state.rhythmClock != nil {
		controls, err := state.rhythmClock.Evaluate(state.rhythmOrigin)
		if err != nil {
			return err
		}
		if err := state.applyRhythm(controls, state.rhythmOrigin); err != nil {
			return err
		}
	}
	return nil
}

func (state *controlState) applyMappings(name string, values []float64, at time.Time) error {
	for _, binding := range state.bindings {
		if binding.control != name {
			continue
		}
		if err := binding.apply(state, values, at); err != nil {
			return fmt.Errorf("map control %s to %s: %w", name, binding.target.Name, err)
		}
	}
	return nil
}

func (binding *mappingBinding) apply(state *controlState, values []float64, at time.Time) error {
	count := len(state.model.Voices)
	if binding.target.EffectIndex >= 0 {
		if len(values) != 1 {
			return fmt.Errorf("vector control with %d values cannot address global effect target", len(values))
		}
		count = 1
	} else if len(values) != 1 && len(values) != count {
		return fmt.Errorf("%d control values cannot address %d voices", len(values), count)
	}
	if len(binding.mappers) == 0 {
		binding.mappers = make([]*control.Mapper, count)
		binding.last = make([]time.Time, count)
		binding.hasLast = make([]bool, count)
		for index := 0; index < count; index++ {
			voiceIndex := index
			if binding.target.EffectIndex >= 0 {
				voiceIndex = 0
			}
			initial, err := binding.target.Value(state.model, voiceIndex)
			if err != nil {
				return err
			}
			mapper, err := control.NewMapper(binding.mapping, binding.input, initial)
			if err != nil {
				return err
			}
			binding.mappers[index] = mapper
		}
	}
	for index, mapper := range binding.mappers {
		valueIndex := index
		if len(values) == 1 {
			valueIndex = 0
		}
		delta := time.Duration(0)
		if binding.hasLast[index] {
			if at.Before(binding.last[index]) {
				return fmt.Errorf("sample timestamp moved backwards")
			}
			delta = at.Sub(binding.last[index])
		}
		mapped, err := mapper.Step(values[valueIndex], delta)
		if err != nil {
			return err
		}
		binding.last[index] = at
		binding.hasLast[index] = true
		voiceIndex := index
		if binding.target.EffectIndex >= 0 {
			voiceIndex = 0
		}
		if err := state.update(binding.target, voiceIndex, mapped); err != nil {
			return err
		}
	}
	return nil
}

func (state *controlState) update(target sound.Target, voiceIndex int, value float64) error {
	if err := target.Set(&state.model, voiceIndex, value); err != nil {
		return err
	}
	if target.Name == "gate" && target.EffectIndex < 0 {
		state.gateValues[voiceIndex] = value
	}
	if state.session != nil {
		ctx := state.updateContext
		if ctx == nil {
			ctx = context.Background()
		}
		if err := state.session.Update(ctx, audio.Update{Target: target, VoiceIndex: voiceIndex, Value: value}); err != nil {
			return fmt.Errorf("update backend: %w", err)
		}
	}
	return nil
}

func (state *controlState) evaluateTrigger(values []float64) ([]int, error) {
	if state.trigger.scalar != nil {
		if len(values) != 1 {
			return nil, fmt.Errorf("scalar trigger received %d values", len(values))
		}
		active, err := state.trigger.scalar.Evaluate(values[0])
		if err != nil || !active {
			return nil, err
		}
		indices := make([]int, len(state.model.Voices))
		for index := range indices {
			indices[index] = index
		}
		return indices, nil
	}
	if len(values) != len(state.model.Voices) {
		return nil, fmt.Errorf("vector trigger received %d values for %d voices", len(values), len(state.model.Voices))
	}
	active, err := state.trigger.vector.Evaluate(values)
	if err != nil {
		return nil, err
	}
	var indices []int
	for index, on := range active {
		if on {
			indices = append(indices, index)
		}
	}
	return indices, nil
}

func (state *controlState) applyRhythm(controls primitive.RhythmControls, at time.Time) error {
	// Apply rhythm's inherent articulation first. Explicit modulation bindings
	// are evaluated afterward so a named rhythm control remains authoritative
	// when the user routes it to gate, frequency, or an effect target.
	if state.trigger == nil {
		for index := range state.model.Voices {
			if state.gateValues[index] != controls.Gate {
				if err := state.setGate(index, controls.Gate); err != nil {
					return err
				}
			}
		}
	}
	if controls.Hit == 1 && state.plan.SourceEntry.Info.Kind == source.KindScalar && len(state.plan.Command.Notes) > 1 {
		note := state.plan.Command.Notes[controls.Step%len(state.plan.Command.Notes)]
		if err := state.update(sound.Target{Name: "freq", EffectIndex: -1}, 0, note.Frequency()); err != nil {
			return err
		}
	}

	values := []struct {
		name  string
		value float64
	}{
		{name: "rhythm.gate", value: controls.Gate},
		{name: "rhythm.hit", value: controls.Hit},
		{name: "rhythm.step", value: float64(controls.Step)},
		{name: "rhythm.velocity", value: controls.Velocity},
		{name: "rhythm.phase", value: controls.Phase},
	}
	for _, item := range values {
		if err := state.applyMappings(item.name, []float64{item.value}, at); err != nil {
			return err
		}
	}
	return nil
}

func (state *controlState) setGate(index int, value float64) error {
	return state.update(sound.Target{Name: "gate", EffectIndex: -1}, index, value)
}

func (state *controlState) scheduleInitialGates(ctx context.Context, clock Clock, events chan<- runtimeEvent) {
	if state.trigger == nil {
		return
	}
	for index, generation := range state.gateGen {
		if generation > 0 && state.gateValues[index] > 0 {
			scheduleGate(ctx, clock, state.plan.GateDuration, index, generation, events)
		}
	}
}

func scheduleGate(ctx context.Context, clock Clock, duration time.Duration, voiceIndex int, generation uint64, events chan<- runtimeEvent) {
	go func() {
		select {
		case <-ctx.Done():
		case at := <-clock.After(duration):
			sendEvent(ctx, events, runtimeEvent{kind: eventGateOff, at: at, voiceIndex: voiceIndex, generation: generation})
		}
	}()
}

func (state *controlState) run(ctx context.Context, clock Clock, events chan runtimeEvent) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-events:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			switch event.kind {
			case eventRenderer:
				if event.err != nil {
					return fmt.Errorf("audio renderer stopped: %w", event.err)
				}
				return fmt.Errorf("audio renderer stopped unexpectedly")
			case eventSource:
				if event.err != nil {
					if errors.Is(event.err, io.EOF) && event.name == state.plan.Command.Source {
						return nil
					}
					if errors.Is(event.err, io.EOF) {
						return fmt.Errorf("control %s ended", event.name)
					}
					return fmt.Errorf("collect control %s: %w", event.name, event.err)
				}
				if err := state.applyMappings(event.name, event.values, event.at); err != nil {
					return err
				}
				if event.name == state.plan.Command.Source && state.trigger != nil {
					active, err := state.evaluateTrigger(event.values)
					if err != nil {
						return err
					}
					for _, index := range active {
						state.gateGen[index]++
						generation := state.gateGen[index]
						if state.gateValues[index] != 1 {
							if err := state.setGate(index, 1); err != nil {
								return err
							}
						}
						scheduleGate(ctx, clock, state.plan.GateDuration, index, generation, events)
					}
				}
			case eventRhythm:
				controls, err := state.rhythmClock.Evaluate(event.at)
				if err != nil {
					return err
				}
				if err := state.applyRhythm(controls, event.at); err != nil {
					return err
				}
			case eventGateOff:
				if event.voiceIndex >= 0 && event.voiceIndex < len(state.gateGen) && state.gateGen[event.voiceIndex] == event.generation {
					if err := state.setGate(event.voiceIndex, 0); err != nil {
						return err
					}
				}
			}
		}
	}
}
