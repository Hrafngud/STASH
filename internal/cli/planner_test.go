package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
)

type plannerCollector struct{}

func (plannerCollector) Collect(context.Context) (source.Sample, error) { return nil, nil }

func plannerRegistry(t *testing.T) *source.Registry {
	t.Helper()
	registry := source.NewRegistry()
	factory := func(context.Context) (source.Collector, error) { return plannerCollector{}, nil }
	register := func(name string, natural bool) {
		t.Helper()
		info := source.Info{Name: name, Kind: source.KindScalar, Unit: "%"}
		if natural {
			minimum, maximum := 0.0, 100.0
			info.NaturalMin, info.NaturalMax = &minimum, &maximum
		}
		if err := registry.RegisterAvailable(info, factory); err != nil {
			t.Fatal(err)
		}
	}
	register("cpu.usage", true)
	register("cpu.no-range", false)
	register("io.no-range", false)
	register("-", false)
	info := source.Info{Name: "cpu.temp", Kind: source.KindScalar, Unit: "C"}
	minimum, maximum := 0.0, 120.0
	info.NaturalMin, info.NaturalMax = &minimum, &maximum
	if err := registry.RegisterUnavailable(info, "no reliable sensor"); err != nil {
		t.Fatal(err)
	}
	return registry
}

func parsePlan(t *testing.T, registry *source.Registry, args ...string) (cli.Plan, error) {
	t.Helper()
	command, err := cli.Parse(args)
	if err != nil {
		return cli.Plan{}, err
	}
	return cli.BuildPlan(command, registry)
}

func TestBuildPlanSelectsEveryMode(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	tests := []struct {
		name string
		args []string
		mode cli.Mode
	}{
		{name: "list", args: []string{"-l", "cpu"}, mode: cli.ModeList},
		{name: "inspect", args: []string{"-i", "cpu.temp"}, mode: cli.ModeInspect},
		{name: "primitive", args: []string{"-p", "scale:C4:major:3"}, mode: cli.ModePrimitive},
		{name: "telemetry", args: []string{"cpu.usage"}, mode: cli.ModeTelemetry},
		{name: "audio device", args: []string{"cpu.usage", "-m", "freq=80..2k"}, mode: cli.ModeAudioDevice},
		{name: "raw PCM", args: []string{"cpu.usage", "-o", "-"}, mode: cli.ModeRawPCM},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan, err := parsePlan(t, registry, test.args...)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Mode != test.mode {
				t.Fatalf("mode = %q, want %q", plan.Mode, test.mode)
			}
		})
	}
}

func TestEveryAudioProducingOptionActivatesAudio(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	tests := [][]string{
		{"-w", "sine"},
		{"-m", "freq=80..2k"},
		{"--range", "0..100"},
		{"-v", ".2"},
		{"-t", "above:50"},
		{"-n", "C4"},
		{"-r", "rhythm:120:1/8:x-"},
		{"-r", "rhythm:1/8:x-", "-b", "120"},
		{"-d", "100ms"},
		{"-a", "5ms,20ms,.8,50ms"},
		{"-r", "rhythm:120:1/8:x-", "--swing", "58"},
		{"-f", "lp:2k"},
		{"-x", "drive:.2"},
	}
	for _, options := range tests {
		options := options
		t.Run(strings.Join(options, "_"), func(t *testing.T) {
			t.Parallel()
			args := append([]string{"cpu.usage"}, options...)
			plan, err := parsePlan(t, registry, args...)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Mode != cli.ModeAudioDevice {
				t.Fatalf("options %q selected %q", options, plan.Mode)
			}
		})
	}
}

func TestBuildPlanAppliesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	defaults, err := parsePlan(t, registry, "cpu.usage")
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Waveform != cli.WaveSine || defaults.Gain != 0.1 ||
		defaults.GateDuration != 100*time.Millisecond || defaults.Swing != primitive.DefaultSwing ||
		defaults.Envelope != cli.DefaultADSR {
		t.Fatalf("defaults = %#v", defaults)
	}

	overrides, err := parsePlan(t, registry,
		"cpu.usage", "-w", "noise", "-v", ".4", "-d", "250ms",
		"-a", "1ms,2ms,.5,3ms", "-r", "rhythm:90:1/8:x-", "--swing", "60",
	)
	if err != nil {
		t.Fatal(err)
	}
	if overrides.Waveform != cli.WaveNoise || overrides.Gain != 0.4 ||
		overrides.GateDuration != 250*time.Millisecond || overrides.Swing != 60 ||
		overrides.Envelope != (cli.ADSR{Attack: time.Millisecond, Decay: 2 * time.Millisecond, Sustain: .5, Release: 3 * time.Millisecond}) ||
		overrides.BPM == nil || *overrides.BPM != 90 {
		t.Fatalf("overrides = %#v", overrides)
	}
}

func TestBuildPlanResolvesPrimitives(t *testing.T) {
	t.Parallel()
	notePlan, err := parsePlan(t, nil, "-p", "mode:E3:phrygian:4")
	if err != nil {
		t.Fatal(err)
	}
	if len(notePlan.Primitive.Notes) != 4 || notePlan.Primitive.Notes[0].String() != "E3" {
		t.Fatalf("note primitive = %#v", notePlan.Primitive)
	}
	rhythmPlan, err := parsePlan(t, nil, "-p", "rhythm:120:1/8:x-x-")
	if err != nil {
		t.Fatal(err)
	}
	if rhythmPlan.Primitive.Rhythm == nil || rhythmPlan.Primitive.BPM == nil || *rhythmPlan.Primitive.BPM != 120 {
		t.Fatalf("rhythm primitive = %#v", rhythmPlan.Primitive)
	}
}

func TestBuildPlanValidatesPlannerRelationships(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown primary source", args: []string{"cpu.missing"}, want: "unknown source"},
		{name: "unavailable primary source", args: []string{"cpu.temp"}, want: "unavailable on this system"},
		{name: "unknown inspect source", args: []string{"-i", "cpu.missing"}, want: "unknown source"},
		{name: "BPM without rhythm", args: []string{"cpu.usage", "-b", "120"}, want: "requires -r"},
		{name: "swing without rhythm", args: []string{"cpu.usage", "--swing", "55"}, want: "requires -r"},
		{name: "rhythm without BPM", args: []string{"cpu.usage", "-r", "rhythm:1/8:x-"}, want: "provide -b BPM"},
		{name: "primitive rhythm without BPM", args: []string{"-p", "rhythm:1/8:x-"}, want: "provide -b BPM"},
		{name: "unknown target", args: []string{"cpu.usage", "-m", "pitch=80..2k"}, want: "unknown modulation target"},
		{name: "rhythm control without rhythm", args: []string{"cpu.usage", "-m", "rhythm.gate:gain=0..1"}, want: "requires -r"},
		{name: "unknown rhythm control", args: []string{"cpu.usage", "-r", "rhythm:120:1/8:x-", "-m", "rhythm.accent:gain=0..1"}, want: "unknown rhythm control"},
		{name: "unknown source control", args: []string{"cpu.usage", "-m", "gpu.usage:gain=0..1"}, want: "unknown source"},
		{name: "unavailable source control", args: []string{"cpu.usage", "-m", "cpu.temp:gain=0..1"}, want: "unavailable on this system"},
		{name: "missing natural range", args: []string{"cpu.no-range", "-m", "freq=80..2k"}, want: "has no natural range"},
		{name: "bad explicit range control", args: []string{"cpu.usage", "--range", "gpu.usage=0..1"}, want: "unknown source"},
		{name: "filter target without filter", args: []string{"cpu.usage", "-m", "filter.cutoff=80..2k"}, want: "requires a declared filter"},
		{name: "delay target without delay", args: []string{"cpu.usage", "-m", "delay.mix=0..1"}, want: "requires a declared delay"},
		{name: "drive target without drive", args: []string{"cpu.usage", "-m", "drive.amount=0..1"}, want: "requires a declared drive"},
		{name: "gain mapping out of range", args: []string{"cpu.usage", "-m", "gain=0..2"}, want: "invalid mapping range"},
		{name: "frequency mapping includes zero", args: []string{"cpu.usage", "-m", "freq=0..1"}, want: "greater than zero"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePlan(t, registry, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("plan %q error = %v, want substring %q", test.args, err, test.want)
			}
		})
	}
}

func TestBuildPlanAcceptsRangesAndAllDocumentedControlsAndTargets(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	tests := [][]string{
		{"-", "--range", "0..1", "-m", "freq=80..2k"},
		{"cpu.no-range", "--range", "cpu.no-range=0..1", "-m", "freq=80..2k"},
		{"cpu.no-range", "--range", "0..1", "--range", "io.no-range=0..2", "-m", "freq=80..2k", "-m", "io.no-range:gain=0..1"},
		{"cpu.usage", "-r", "rhythm:120:1/8:x-", "-m", "rhythm.gate:gate=0..1"},
		{"cpu.usage", "-r", "rhythm:120:1/8:x-", "-m", "rhythm.hit:freq=80..2k"},
		{"cpu.usage", "-r", "rhythm:120:1/8:x-", "-m", "rhythm.step:pan=-1..1"},
		{"cpu.usage", "-r", "rhythm:120:1/8:x-", "-f", "lp:1k", "-m", "rhythm.velocity:filter.cutoff=80..2k"},
		{"cpu.usage", "-r", "rhythm:120:1/8:x-", "-f", "hp:80", "-m", "rhythm.phase:filter.q=.1..1"},
		{"cpu.usage", "-x", "delay:100ms,.2,.3", "-m", "delay.time=1..2"},
		{"cpu.usage", "-x", "delay:100ms,.2,.3", "-m", "delay.time=10ms..200ms"},
		{"cpu.usage", "-x", "flanger:rate=.2,depth=5ms,feedback=.3", "-m", "flanger.depth=1ms..15ms"},
		{"cpu.usage", "-x", "delay:100ms,.2,.3", "-m", "delay.feedback=0..0.9"},
		{"cpu.usage", "-x", "delay:100ms,.2,.3", "-m", "delay.mix=0..1"},
		{"cpu.usage", "-x", "drive:.2", "-m", "drive.amount=0..1"},
	}
	for _, args := range tests {
		if _, err := parsePlan(t, registry, args...); err != nil {
			t.Errorf("plan %q: %v", args, err)
		}
	}
}

func TestBuildPlanConstructsPersistentVoiceAndOrderedEffects(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	plan, err := parsePlan(t, registry,
		"cpu.usage", "-w", "saw", "-v", ".25", "-a", "1ms,2ms,.5,3ms",
		"-f", "lp:2k", "-x", "drive:.2", "-f", "hp:80", "-x", "delay:150ms,.4,.25",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Sound.Voices) != 1 {
		t.Fatalf("voices = %#v", plan.Sound.Voices)
	}
	voice := plan.Sound.Voices[0]
	if voice.Waveform != sound.WaveSaw || voice.Frequency != 440 || voice.Gain != .25 ||
		voice.Pan != 0 || voice.Gate != 1 || voice.Envelope.Attack != time.Millisecond {
		t.Fatalf("voice = %#v", voice)
	}
	want := []sound.EffectKind{
		sound.EffectLowPass, sound.EffectDrive, sound.EffectHighPass, sound.EffectDelay,
	}
	if len(plan.Sound.Effects) != len(want) {
		t.Fatalf("effects = %#v", plan.Sound.Effects)
	}
	for index, kind := range want {
		if plan.Sound.Effects[index].Kind != kind {
			t.Fatalf("effect %d = %q, want %q", index, plan.Sound.Effects[index].Kind, kind)
		}
	}
}

func TestBuildPlanResolvesMostRecentMatchingEffects(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	plan, err := parsePlan(t, registry,
		"cpu.usage",
		"-f", "lp:1k", "-x", "delay:100ms,.1,.2", "-f", "hp:80", "-x", "delay:200ms,.2,.3",
		"-m", "filter.cutoff=100..2k", "-m", "delay.mix=0..1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.SoundTargets) != 2 || plan.SoundTargets[0].EffectIndex != 2 || plan.SoundTargets[1].EffectIndex != 3 {
		t.Fatalf("targets = %#v", plan.SoundTargets)
	}
}

func TestInspectAllowsKnownUnavailableSource(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	plan, err := parsePlan(t, registry, "-i", "cpu.temp")
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourceEntry.Available || plan.SourceEntry.UnavailableReason == "" {
		t.Fatalf("inspect entry = %#v", plan.SourceEntry)
	}
}

func TestUnavailableErrorsRemainTyped(t *testing.T) {
	t.Parallel()
	registry := plannerRegistry(t)
	_, err := parsePlan(t, registry, "cpu.temp")
	var unavailable *source.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %v, want *source.UnavailableError", err)
	}
}
