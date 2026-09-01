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
		target, err := sound.ResolveTarget(plan.Sound.Effects, modulation.Target)
		if err != nil {
			return Plan{}, err
		}
		if err := target.ValidateRange(modulation.Mapping.Output); err != nil {
			return Plan{}, err
		}
		plan.SoundTargets = append(plan.SoundTargets, target)
		controlName := modulation.Control
		if controlName == "" {
			controlName = command.Source
		}
		controlEntry, isRhythm, err := validateControl(registry, controlName, command.Rhythm != nil)
		if err != nil {
			return Plan{}, fmt.Errorf("modulation control %s: %w", controlName, err)
		}
		if !isRhythm && controlEntry.Info.NaturalMin == nil && !hasRangeOverride(command, controlName) {
			return Plan{}, fmt.Errorf("control %s has no natural range; provide --range for it", controlName)
		}
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
	return command.Waveform != nil || len(command.Modulations) > 0 ||
		len(command.RangeOverrides) > 0 || command.Gain != nil || command.Trigger != nil ||
		command.Notes != nil || command.Rhythm != nil || command.BPM != nil ||
		command.GateDuration != nil || command.Envelope != nil || command.Swing != nil ||
		len(command.Ordered) > 0 || command.Output != nil
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
