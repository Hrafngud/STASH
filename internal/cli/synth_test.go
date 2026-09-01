package cli_test

import (
	"strings"
	"testing"

	"github.com/zalmo/stash/internal/cli"
)

func TestSynthDeclarationsTargetsAndAudioRoutes(t *testing.T) {
	registry := plannerRegistry(t)
	command, err := cli.Parse([]string{
		"cpu.usage", "-s", "fm:mod,mix=0,ratio=.25", "-s", "sub:voice,wave=saw",
		"-m", "freq=80..220", "-m", "syn.mod.out:syn.voice.freq.mod=-300..300",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != cli.ModeAudioDevice || len(plan.Sound.Synths) != 2 || len(plan.Sound.AudioRoutes) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.SoundTargets[0].SynthIndex != 1 || plan.SoundTargets[1].SynthIndex != 1 || !plan.SoundTargets[1].Mod {
		t.Fatalf("targets = %#v", plan.SoundTargets)
	}
}

func TestSynthGraphValidation(t *testing.T) {
	registry := plannerRegistry(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"duplicate id", []string{"cpu.usage", "-s", "fm:a", "-s", "sub:a"}, "duplicate synth id"},
		{"cycle", []string{"cpu.usage", "-s", "sub:a", "-s", "sub:b", "-m", "syn.a.out:syn.b.freq.mod=-1..1", "-m", "syn.b.out:syn.a.freq.mod=-1..1"}, "graph contains cycle"},
		{"frequency conflict", []string{"cpu.usage", "-s", "fm:a,ratio=2", "-m", "cpu.usage:syn.a.modfreq=100..200"}, "both ratio and modfreq"},
		{"missing table", []string{"cpu.usage", "-s", "wavetable:lead"}, "requires table"},
		{"unknown parameter", []string{"cpu.usage", "-s", "fm:a,cutoff=2k"}, "has no parameter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := cli.Parse(test.args)
			if err == nil {
				_, err = cli.BuildPlan(command, registry)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestGranularTimeMap(t *testing.T) {
	registry := plannerRegistry(t)
	command, err := cli.Parse([]string{"cpu.usage", "-s", "granular:g,sample=voice.wav", "-m", "syn.g.size=5ms..150ms"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Command.Modulations[0].Mapping.Output.Max; got != .15 {
		t.Fatalf("time range maximum = %g", got)
	}
}

func TestExplicitSynthUsesVAsMasterGain(t *testing.T) {
	registry := plannerRegistry(t)
	command, err := cli.Parse([]string{"cpu.usage", "-s", "sub:voice,gain=.4", "-v", "0"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Sound.MasterGainSet || plan.Sound.MasterGain != 0 || plan.Sound.Synths[0].Parameters["gain"] != .4 {
		t.Fatalf("sound model = %#v", plan.Sound)
	}
}
