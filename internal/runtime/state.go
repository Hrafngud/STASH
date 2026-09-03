package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
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
	plan           cli.Plan
	model          sound.Model
	session        audio.Session
	updateContext  context.Context
	bindings       []*mappingBinding
	trigger        *triggerBinding
	rhythmClock    *primitive.RhythmClock
	rhythmOrigin   time.Time
	latestPrimary  []float64
	gateValues     []float64
	gateGen        []uint64
	gateControlled []bool
}

type mappingBinding struct {
	control string
	target  sound.Target
	mapping control.Mapping
	input   unit.Range
	mappers []*control.Mapper
	last    []time.Time
	hasLast []bool
	current float64
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
		plan:           plan,
		model:          model,
		latestPrimary:  append([]float64(nil), prepared[plan.Command.Source].values...),
		gateValues:     make([]float64, soundNodeCount(model)),
		gateGen:        make([]uint64, soundNodeCount(model)),
		gateControlled: make([]bool, soundNodeCount(model)),
	}
	for index := range state.gateValues {
		if len(model.Synths) > 0 {
			state.gateValues[index] = model.Synths[index].Parameters["gate"]
			if model.Synths[index].Parameters["mix"] == 0 {
				state.gateControlled[index] = true
			}
		} else {
			state.gateValues[index] = model.Voices[index].Gate
		}
	}

	for index, modulation := range plan.Command.Modulations {
		name := modulation.Control
		if name == "" {
			name = plan.Command.Source
		}
		if strings.HasPrefix(name, "syn.") && strings.HasSuffix(name, ".out") {
			continue
		}
		input, err := state.inputRange(name, prepared)
		if err != nil {
			return nil, fmt.Errorf("modulation control %s: %w", name, err)
		}
		state.bindings = append(state.bindings, &mappingBinding{
			control: name, target: plan.SoundTargets[index], mapping: modulation.Mapping, input: input,
		})
		target := plan.SoundTargets[index]
		if target.Name == "gate" {
			if target.IsSynth {
				state.gateControlled[target.SynthIndex] = true
			} else if target.EffectIndex < 0 {
				for node := range state.gateControlled {
					state.gateControlled[node] = true
				}
			}
		}
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
	for _, override := range state.plan.Command.RangeOverrides {
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
		if name == state.plan.Command.Source && state.trigger != nil && !state.rhythmVectorTrigger() {
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
		if _, err := state.applyRhythm(controls, state.rhythmOrigin); err != nil {
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
	count := soundNodeCount(state.model)
	if binding.target.Vector {
		count, _ = strconv.Atoi(state.model.Synths[binding.target.SynthIndex].Config["partials"])
		if len(values) != count {
			return fmt.Errorf("vector control with %d values cannot address %d additive partials", len(values), count)
		}
	} else if binding.target.EffectIndex >= 0 || binding.target.IsSynth {
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
			if (binding.target.EffectIndex >= 0 || binding.target.IsSynth) && !binding.target.Vector {
				voiceIndex = 0
			}
			initial, err := binding.target.Value(state.model, voiceIndex)
			if err != nil {
				return err
			}
			if binding.target.IsSynth && binding.target.Mod {
				initial = 0
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
		if (binding.target.EffectIndex >= 0 || binding.target.IsSynth) && !binding.target.Vector {
			voiceIndex = 0
		}
		if binding.target.IsSynth && binding.target.Mod {
			binding.current = mapped
			total := 0.0
			for _, other := range state.bindings {
				if other.target.IsSynth && other.target.Mod && other.target.SynthIndex == binding.target.SynthIndex && other.target.Name == binding.target.Name {
					total += other.current
				}
			}
			mapped = total
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
		if target.IsSynth {
			voiceIndex = target.SynthIndex
		}
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
		indices := make([]int, soundNodeCount(state.model))
		for index := range indices {
			indices[index] = index
		}
		return indices, nil
	}
	if len(values) != soundNodeCount(state.model) {
		return nil, fmt.Errorf("vector trigger received %d values for %d voices", len(values), soundNodeCount(state.model))
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

func (state *controlState) applyRhythm(controls primitive.RhythmControls, at time.Time) ([]int, error) {
	var activated []int
	// Apply rhythm's inherent articulation first. Explicit modulation bindings
	// are evaluated afterward so a named rhythm control remains authoritative
	// when the user routes it to gate, frequency, or an effect target.
	if state.rhythmVectorTrigger() {
		if controls.Hit == 1 {
			indices, err := state.evaluateTrigger(state.latestPrimary)
			if err != nil {
				return nil, err
			}
			selected := make([]bool, soundNodeCount(state.model))
			for _, index := range indices {
				selected[index] = true
			}
			for index := range state.gateValues {
				if selected[index] {
					state.gateGen[index]++
					// A zero-to-one transition retriggers the persistent voice even
					// when the preceding note's gate duration overlaps this hit.
					if state.gateValues[index] != 0 {
						if err := state.setGate(index, 0); err != nil {
							return nil, err
						}
					}
					if err := state.setGate(index, 1); err != nil {
						return nil, err
					}
					activated = append(activated, index)
				} else if state.gateValues[index] != 0 {
					state.gateGen[index]++
					if err := state.setGate(index, 0); err != nil {
						return nil, err
					}
				}
			}
		} else if controls.Gate == 0 {
			// Rest steps end any note whose configured gate duration extends
			// beyond the hit step, without creating a new trigger evaluation.
			for index := range state.gateValues {
				if state.gateValues[index] != 0 {
					state.gateGen[index]++
					if err := state.setGate(index, 0); err != nil {
						return nil, err
					}
				}
			}
		}
	} else if state.trigger == nil {
		for index := range state.gateValues {
			if state.gateValues[index] != controls.Gate {
				if err := state.setGate(index, controls.Gate); err != nil {
					return nil, err
				}
			}
		}
	}
	if controls.Hit == 1 && state.plan.SourceEntry.Info.Kind == source.KindScalar && len(state.plan.Command.Notes) > 1 {
		note := state.plan.Command.Notes[controls.Step%len(state.plan.Command.Notes)]
		target, err := sound.ResolveModelTarget(state.model, "freq")
		if err != nil {
			return nil, err
		}
		if err := state.update(target, 0, note.Frequency()); err != nil {
			return nil, err
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
			return nil, err
		}
	}
	return activated, nil
}

func (state *controlState) rhythmVectorTrigger() bool {
	return state.trigger != nil && state.rhythmClock != nil && state.plan.SourceEntry.Info.Kind == source.KindVector
}

func (state *controlState) setGate(index int, value float64) error {
	if index >= 0 && index < len(state.gateControlled) && state.gateControlled[index] {
		return nil
	}
	if len(state.model.Synths) > 0 {
		return state.update(sound.Target{Name: "gate", EffectIndex: -1, IsSynth: true, SynthIndex: index}, 0, value)
	}
	return state.update(sound.Target{Name: "gate", EffectIndex: -1}, index, value)
}

func soundNodeCount(model sound.Model) int {
	if len(model.Synths) > 0 {
		return len(model.Synths)
	}
	return len(model.Voices)
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
				if event.name == state.plan.Command.Source {
					state.latestPrimary = append(state.latestPrimary[:0], event.values...)
				}
				if event.name == state.plan.Command.Source && state.trigger != nil && !state.rhythmVectorTrigger() {
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
				activated, err := state.applyRhythm(controls, event.at)
				if err != nil {
					return err
				}
				for _, index := range activated {
					scheduleGate(ctx, clock, state.plan.GateDuration, index, state.gateGen[index], events)
				}
			case eventGateOff:
				if event.voiceIndex >= 0 && event.voiceIndex < len(state.gateGen) && state.gateGen[event.voiceIndex] == event.generation {
					if err := state.setGate(event.voiceIndex, 0); err != nil {
						return err
					}
				}
			case eventExternal:
				for _, update := range event.updates {
					if err := state.update(update.Target, update.VoiceIndex, update.Value); err != nil {
						return fmt.Errorf("apply live update: %w", err)
					}
				}
			}
		}
	}
}
