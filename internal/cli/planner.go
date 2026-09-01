package cli

import (
	"fmt"
	"strings"

	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
)

var rhythmControls = map[string]struct{}{
	"rhythm.gate": {}, "rhythm.hit": {}, "rhythm.step": {},
	"rhythm.velocity": {}, "rhythm.phase": {},
}

// BuildPlan validates source/control references and cross-option requirements,
// selects one execution mode, and applies documented audio defaults.
func BuildPlan(command Command, registry *source.Registry) (Plan, error) {
	plan := Plan{
		Command:      command,
		Waveform:     WaveSine,
		Gain:         DefaultGain,
		GateDuration: DefaultGateDuration,
		Envelope:     DefaultADSR,
		Swing:        primitive.DefaultSwing,
	}

	switch command.Kind {
	case CommandList:
		plan.Mode = ModeList
		return plan, nil
	case CommandInspect:
		if strings.HasPrefix(command.InspectSource, "syn.") {
			kind := sound.SynthType(strings.TrimPrefix(command.InspectSource, "syn."))
			spec, ok := sound.LookupSynthSpec(kind)
			if !ok {
				return Plan{}, fmt.Errorf("unknown synth: %s", kind)
			}
			plan.Mode, plan.SynthSpec = ModeInspect, &spec
			return plan, nil
		}
		entry, err := lookupKnown(registry, command.InspectSource)
		if err != nil {
			return Plan{}, err
		}
		plan.Mode, plan.SourceEntry, plan.HasSource = ModeInspect, entry, true
		return plan, nil
	case CommandPrimitive:
		resolution, err := resolvePrimitive(command.Primitive)
		if err != nil {
			return Plan{}, err
		}
		plan.Mode, plan.Primitive = ModePrimitive, resolution
		return plan, nil
	case CommandSource:
		// Continue below.
	default:
		return Plan{}, fmt.Errorf("invalid command kind %d", command.Kind)
	}

	entry, err := lookupAvailable(registry, command.Source)
	if err != nil {
		return Plan{}, err
	}
	plan.SourceEntry, plan.HasSource = entry, true

	if command.Waveform != nil {
		plan.Waveform = *command.Waveform
	}
	if command.Gain != nil {
		plan.Gain = *command.Gain
	}
	if command.GateDuration != nil {
		plan.GateDuration = *command.GateDuration
	}
	if command.Envelope != nil {
		plan.Envelope = *command.Envelope
	}
	if command.Swing != nil {
		plan.Swing = *command.Swing
	}
	voice := sound.DefaultVoice()
	voice.Waveform = plan.Waveform
	voice.Gain = plan.Gain
	voice.Envelope = plan.Envelope
	plan.Sound = sound.Model{
		Voices:  []sound.Voice{voice},
		Effects: append([]sound.Effect(nil), command.Effects...),
	}
	if len(command.Synths) > 0 {
		plan.Sound.Voices = nil
		plan.Sound.Synths = cloneSynths(command.Synths)
		if err := sound.AssignSynthIDs(plan.Sound.Synths); err != nil {
			return Plan{}, err
		}
		for index := range plan.Sound.Synths {
			plan.Sound.Synths[index].Envelope = plan.Envelope
		}
		plan.Sound.MasterGain = 1
		plan.Sound.MasterGainSet = true
		if command.Gain != nil {
			plan.Sound.MasterGain = *command.Gain
		}
	}
	if err := plan.Sound.Validate(); err != nil {
		return Plan{}, fmt.Errorf("invalid sound model: %w", err)
	}

	if command.Rhythm == nil {
		if command.BPM != nil {
			return Plan{}, fmt.Errorf("option -b requires -r RHYTHM")
		}
		if command.Swing != nil {
			return Plan{}, fmt.Errorf("option --swing requires -r RHYTHM")
		}
	} else {
		bpm, err := command.Rhythm.ResolveBPM(command.BPM)
		if err != nil {
			return Plan{}, err
		}
		plan.BPM = &bpm
	}

	for _, modulation := range command.Modulations {
		target, err := sound.ResolveModelTarget(plan.Sound, modulation.Target)
		if err != nil {
			return Plan{}, err
		}
		if err := validateMappingUnit(plan.Sound, target, modulation.Mapping.OutputUnit); err != nil {
			return Plan{}, err
		}
		if err := target.ValidateModelRange(plan.Sound, modulation.Mapping.Output); err != nil {
			return Plan{}, err
		}
		plan.SoundTargets = append(plan.SoundTargets, target)
		controlName := modulation.Control
		if controlName == "" {
			controlName = command.Source
		}
		if sourceIndex, audioControl := resolveSynthOutput(plan.Sound.Synths, controlName); audioControl {
			if sourceIndex < 0 {
				return Plan{}, fmt.Errorf("invalid control source: %s", controlName)
			}
			if !target.IsSynth {
				return Plan{}, fmt.Errorf("unsupported audio-rate target: %s", modulation.Target)
			}
			if !target.SupportsAudioRate(plan.Sound) {
				return Plan{}, fmt.Errorf("unsupported audio-rate target: %s", modulation.Target)
			}
			plan.Sound.AudioRoutes = append(plan.Sound.AudioRoutes, sound.AudioRoute{SourceIndex: sourceIndex, Target: target, OutputMin: modulation.Mapping.Output.Min, OutputMax: modulation.Mapping.Output.Max, Curve: string(modulation.Mapping.Curve), Smoothing: modulation.Mapping.Smoothing})
			continue
		}
		controlEntry, isRhythm, err := validateControl(registry, controlName, command.Rhythm != nil)
		if err != nil {
			return Plan{}, fmt.Errorf("modulation control %s: %w", controlName, err)
		}
		if !isRhythm && controlEntry.Info.NaturalMin == nil && !hasRangeOverride(command, controlName) {
			return Plan{}, fmt.Errorf("control %s has no natural range; provide --range for it", controlName)
		}
	}
	if err := validateSynthFrequencyDefinitions(plan.Sound, command.Modulations, plan.SoundTargets); err != nil {
		return Plan{}, err
	}
	if err := validateAudioGraph(plan.Sound); err != nil {
		return Plan{}, err
	}
	for _, override := range command.RangeOverrides {
		controlName := override.Control
		if controlName == "" {
			controlName = command.Source
		}
		if _, _, err := validateControl(registry, controlName, command.Rhythm != nil); err != nil {
			return Plan{}, fmt.Errorf("range control %s: %w", controlName, err)
		}
	}

	if command.Output != nil {
		plan.Mode = ModeRawPCM
	} else if activatesAudio(command) {
		plan.Mode = ModeAudioDevice
	} else {
		plan.Mode = ModeTelemetry
	}
	return plan, nil
}

func activatesAudio(command Command) bool {
	return len(command.Synths) > 0 || command.Waveform != nil || len(command.Modulations) > 0 ||
		len(command.RangeOverrides) > 0 || command.Gain != nil || command.Trigger != nil ||
		command.Notes != nil || command.Rhythm != nil || command.BPM != nil ||
		command.GateDuration != nil || command.Envelope != nil || command.Swing != nil ||
		len(command.Ordered) > 0 || command.Output != nil
}

func cloneSynths(input []sound.Synth) []sound.Synth {
	output := make([]sound.Synth, len(input))
	for index, synth := range input {
		output[index] = synth
		output[index].Parameters = make(map[string]float64, len(synth.Parameters))
		for name, value := range synth.Parameters {
			output[index].Parameters[name] = value
		}
		output[index].Modulations = make(map[string]float64, len(synth.Modulations))
		for name, value := range synth.Modulations {
			output[index].Modulations[name] = value
		}
		output[index].Config = make(map[string]string, len(synth.Config))
		for name, value := range synth.Config {
			output[index].Config[name] = value
		}
		output[index].Explicit = make(map[string]bool, len(synth.Explicit))
		for name, value := range synth.Explicit {
			output[index].Explicit[name] = value
		}
	}
	return output
}

func resolveSynthOutput(synths []sound.Synth, name string) (int, bool) {
	if !strings.HasPrefix(name, "syn.") || !strings.HasSuffix(name, ".out") {
		return -1, false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, "syn."), ".out")
	for index, synth := range synths {
		if synth.ID == id {
			return index, true
		}
	}
	return -1, true
}

func validateMappingUnit(model sound.Model, target sound.Target, outputUnit string) error {
	unitName := ""
	if target.IsSynth {
		unitName = model.Synths[target.SynthIndex].Spec().Parameters[target.Name].Unit
	}
	if unitName == "s" && outputUnit != "time" {
		return fmt.Errorf("time-valued target %s requires a time-valued map", target.Name)
	}
	if unitName != "s" && outputUnit == "time" {
		return fmt.Errorf("target %s does not accept a time-valued map", target.Name)
	}
	return nil
}

func validateSynthFrequencyDefinitions(model sound.Model, mods []Modulation, targets []sound.Target) error {
	ratio, modfreq := make([]bool, len(model.Synths)), make([]bool, len(model.Synths))
	for index, synth := range model.Synths {
		ratio[index], modfreq[index] = synth.Explicit["ratio"], synth.Explicit["modfreq"]
	}
	for index, target := range targets {
		if !target.IsSynth {
			continue
		}
		switch target.Name {
		case "ratio":
			ratio[target.SynthIndex] = true
		case "modfreq":
			modfreq[target.SynthIndex] = true
		}
		_ = mods[index]
	}
	for index := range model.Synths {
		if ratio[index] && modfreq[index] {
			return fmt.Errorf("%s synth %s cannot define or target both ratio and modfreq", model.Synths[index].Type, model.Synths[index].ID)
		}
		if modfreq[index] {
			model.Synths[index].Config["_modfreq"] = "true"
		}
	}
	return nil
}

func validateAudioGraph(model sound.Model) error {
	edges := make([][]int, len(model.Synths))
	indegree := make([]int, len(model.Synths))
	for _, route := range model.AudioRoutes {
		edges[route.SourceIndex] = append(edges[route.SourceIndex], route.Target.SynthIndex)
		indegree[route.Target.SynthIndex]++
	}
	queue := []int{}
	for index, degree := range indegree {
		if degree == 0 {
			queue = append(queue, index)
		}
	}
	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range edges[node] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(model.Synths) {
		cycle := modulationCycle(model.Synths, edges)
		return fmt.Errorf("modulation graph contains cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func modulationCycle(synths []sound.Synth, edges [][]int) []string {
	colors := make([]uint8, len(synths))
	stack := []int{}
	cycle := []int{}
	var visit func(int) bool
	visit = func(node int) bool {
		colors[node] = 1
		stack = append(stack, node)
		for _, next := range edges[node] {
			if colors[next] == 0 {
				if visit(next) {
					return true
				}
			} else if colors[next] == 1 {
				start := 0
				for stack[start] != next {
					start++
				}
				cycle = append(cycle, stack[start:]...)
				cycle = append(cycle, next)
				return true
			}
		}
		stack = stack[:len(stack)-1]
		colors[node] = 2
		return false
	}
	for index := range synths {
		if colors[index] == 0 && visit(index) {
			break
		}
	}
	names := make([]string, len(cycle))
	for index, node := range cycle {
		names[index] = synths[node].ID
	}
	return names
}

func lookupKnown(registry *source.Registry, name string) (source.Entry, error) {
	if registry == nil {
		return source.Entry{}, fmt.Errorf("source registry is nil")
	}
	entry, ok := registry.Lookup(name)
	if !ok {
		return source.Entry{}, fmt.Errorf("unknown source: %s", name)
	}
	return entry, nil
}

func lookupAvailable(registry *source.Registry, name string) (source.Entry, error) {
	entry, err := lookupKnown(registry, name)
	if err != nil {
		return source.Entry{}, err
	}
	if !entry.Available {
		return source.Entry{}, &source.UnavailableError{Name: name, Reason: entry.UnavailableReason}
	}
	return entry, nil
}

func validateControl(registry *source.Registry, name string, hasRhythm bool) (source.Entry, bool, error) {
	if strings.HasPrefix(name, "rhythm.") {
		if _, ok := rhythmControls[name]; !ok {
			return source.Entry{}, true, fmt.Errorf("unknown rhythm control %q", name)
		}
		if !hasRhythm {
			return source.Entry{}, true, fmt.Errorf("rhythm control %q requires -r RHYTHM", name)
		}
		return source.Entry{}, true, nil
	}
	entry, err := lookupAvailable(registry, name)
	return entry, false, err
}

func hasRangeOverride(command Command, controlName string) bool {
	for _, override := range command.RangeOverrides {
		overrideControl := override.Control
		if overrideControl == "" {
			overrideControl = command.Source
		}
		if overrideControl == controlName {
			return true
		}
	}
	return false
}

func resolvePrimitive(input string) (PrimitiveResolution, error) {
	if strings.HasPrefix(input, "syn.") {
		synth, err := sound.ParseSynth(strings.TrimPrefix(input, "syn."))
		if err != nil {
			return PrimitiveResolution{}, err
		}
		items := []sound.Synth{synth}
		if err := sound.AssignSynthIDs(items); err != nil {
			return PrimitiveResolution{}, err
		}
		return PrimitiveResolution{Synth: &items[0]}, nil
	}
	if strings.HasPrefix(input, "rhythm:") {
		rhythm, err := primitive.ParseRhythm(input)
		if err != nil {
			return PrimitiveResolution{}, err
		}
		bpm, err := rhythm.ResolveBPM(nil)
		if err != nil {
			return PrimitiveResolution{}, err
		}
		return PrimitiveResolution{Rhythm: &rhythm, BPM: &bpm}, nil
	}
	notes, err := primitive.ParseMaterial(input)
	if err != nil {
		return PrimitiveResolution{}, err
	}
	return PrimitiveResolution{Notes: notes}, nil
}
