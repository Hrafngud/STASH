package cli_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/control"
	"github.com/zalmo/stash/internal/sound"
)

func TestParseDiscoveryForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want cli.Command
	}{
		{name: "list all", args: []string{"-l"}, want: cli.Command{Kind: cli.CommandList}},
		{name: "list prefix", args: []string{"-l", "cpu"}, want: cli.Command{Kind: cli.CommandList, ListPrefix: "cpu"}},
		{name: "inspect", args: []string{"-i", "cpu.usage"}, want: cli.Command{Kind: cli.CommandInspect, InspectSource: "cpu.usage"}},
		{name: "primitive", args: []string{"-p", "C4"}, want: cli.Command{Kind: cli.CommandPrimitive, Primitive: "C4"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := cli.Parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", test.args, got, test.want)
			}
		})
	}
}

func TestParseEverySourceOptionAndRetainsRepeatableOrder(t *testing.T) {
	t.Parallel()
	args := []string{
		"cpu.usage",
		"-w", "saw",
		"-m", "freq=80..2k/exp~150ms",
		"--range", "0..100",
		"-v", ".2",
		"-t", "rise:80",
		"-n", "C4,E4,G4",
		"-r", "rhythm:1/8:x-x-",
		"-b", "120",
		"-d", "150ms",
		"-a", "5ms,40ms,.7,100ms",
		"--swing", "58",
		"-f", "lp:2k",
		"-x", "drive:.2",
		"-m", "rhythm.gate:gain=.05..0.3",
		"-f", "hp:80,.7",
		"-x", "delay:150ms,.4,.25",
		"-o", "-",
	}
	command, err := cli.Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	if command.Kind != cli.CommandSource || command.Source != "cpu.usage" {
		t.Fatalf("source command = %#v", command)
	}
	if command.Waveform == nil || *command.Waveform != cli.WaveSaw {
		t.Fatalf("waveform = %v", command.Waveform)
	}
	if command.Gain == nil || *command.Gain != 0.2 {
		t.Fatalf("gain = %v", command.Gain)
	}
	if command.Trigger == nil || command.Trigger.Kind != control.TriggerRise || command.Trigger.Threshold != 80 {
		t.Fatalf("trigger = %#v", command.Trigger)
	}
	if len(command.Notes) != 3 || command.Notes[0].String() != "C4" || command.Notes[2].String() != "G4" {
		t.Fatalf("notes = %v", command.Notes)
	}
	if command.Rhythm == nil || command.Rhythm.Pattern != "x-x-" || command.BPM == nil || *command.BPM != 120 {
		t.Fatalf("rhythm/BPM = %#v/%v", command.Rhythm, command.BPM)
	}
	if command.GateDuration == nil || *command.GateDuration != 150*time.Millisecond {
		t.Fatalf("gate duration = %v", command.GateDuration)
	}
	if command.Envelope == nil || command.Envelope.Attack != 5*time.Millisecond ||
		command.Envelope.Decay != 40*time.Millisecond || command.Envelope.Sustain != 0.7 ||
		command.Envelope.Release != 100*time.Millisecond {
		t.Fatalf("envelope = %#v", command.Envelope)
	}
	if command.Swing == nil || *command.Swing != 58 {
		t.Fatalf("swing = %v", command.Swing)
	}
	if command.RangeOverride == nil || command.RangeOverride.Control != "" ||
		command.RangeOverride.Range.Min != 0 || command.RangeOverride.Range.Max != 100 {
		t.Fatalf("range override = %#v", command.RangeOverride)
	}
	if command.Output == nil || *command.Output != "-" {
		t.Fatalf("output = %v", command.Output)
	}
	if len(command.Modulations) != 2 || command.Modulations[0].Target != "freq" ||
		command.Modulations[1].Control != "rhythm.gate" || command.Modulations[1].Target != "gain" {
		t.Fatalf("modulations = %#v", command.Modulations)
	}
	wantOrder := []cli.OrderedOption{
		{Kind: cli.OrderedModulation, Argument: "freq=80..2k/exp~150ms"},
		{Kind: cli.OrderedFilter, Argument: "lp:2k"},
		{Kind: cli.OrderedEffect, Argument: "drive:.2"},
		{Kind: cli.OrderedModulation, Argument: "rhythm.gate:gain=.05..0.3"},
		{Kind: cli.OrderedFilter, Argument: "hp:80,.7"},
		{Kind: cli.OrderedEffect, Argument: "delay:150ms,.4,.25"},
	}
	if !reflect.DeepEqual(command.Ordered, wantOrder) {
		t.Fatalf("ordered options = %#v, want %#v", command.Ordered, wantOrder)
	}
	wantEffects := []sound.EffectKind{
		sound.EffectLowPass, sound.EffectDrive, sound.EffectHighPass, sound.EffectDelay,
	}
	if len(command.Effects) != len(wantEffects) {
		t.Fatalf("effect count = %d, want %d", len(command.Effects), len(wantEffects))
	}
	for index, kind := range wantEffects {
		if command.Effects[index].Kind != kind {
			t.Fatalf("effect %d kind = %q, want %q", index, command.Effects[index].Kind, kind)
		}
	}
}

func TestParseExplicitRangeAndModulationControls(t *testing.T) {
	t.Parallel()
	command, err := cli.Parse([]string{
		"cpu.usage",
		"--range", "net.enp4s0.rx=0..100M",
		"-m", "net.enp4s0.rx:filter.cutoff=300..8k/log~50ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := command.RangeOverride.Control; got != "net.enp4s0.rx" {
		t.Fatalf("range control = %q", got)
	}
	modulation := command.Modulations[0]
	if modulation.Control != "net.enp4s0.rx" || modulation.Target != "filter.cutoff" ||
		modulation.Mapping.Curve != control.CurveLog || modulation.Mapping.Smoothing != 50*time.Millisecond {
		t.Fatalf("modulation = %#v", modulation)
	}
}

func TestParseRejectsMalformedArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing command", want: "missing command"},
		{name: "option before source", args: []string{"-w", "sine"}, want: "expected SOURCE"},
		{name: "unknown source option", args: []string{"cpu.usage", "--loud"}, want: "unknown option"},
		{name: "extra positional", args: []string{"cpu.usage", "other"}, want: "unexpected positional"},
		{name: "list extras", args: []string{"-l", "cpu", "extra"}, want: "at most one"},
		{name: "list combined option", args: []string{"-l", "-w"}, want: "cannot be combined"},
		{name: "list unknown option", args: []string{"-l", "--bogus"}, want: "unknown option"},
		{name: "inspect missing", args: []string{"-i"}, want: "requires exactly one"},
		{name: "primitive extras", args: []string{"-p", "C4", "extra"}, want: "requires exactly one"},
		{name: "discovery with source", args: []string{"cpu.usage", "-l"}, want: "cannot be combined"},
		{name: "missing value", args: []string{"cpu.usage", "-w"}, want: "requires a value"},
		{name: "missing value before option", args: []string{"cpu.usage", "-w", "-v", ".2"}, want: "option -w requires a value"},
		{name: "empty value", args: []string{"cpu.usage", "-f", ""}, want: "must not be empty"},
		{name: "unknown waveform", args: []string{"cpu.usage", "-w", "pulse"}, want: "unknown waveform"},
		{name: "bad modulation equals", args: []string{"cpu.usage", "-m", "freq=1..2=3"}, want: "invalid modulation"},
		{name: "bad modulation colon", args: []string{"cpu.usage", "-m", "cpu:usage:freq=1..2"}, want: "control separator"},
		{name: "bad mapping", args: []string{"cpu.usage", "-m", "freq=1..2/cubic"}, want: "unknown curve"},
		{name: "bad range override", args: []string{"cpu.usage", "--range", "80...2k"}, want: "invalid range override"},
		{name: "gain low", args: []string{"cpu.usage", "-v", "-.1"}, want: "invalid gain"},
		{name: "gain high", args: []string{"cpu.usage", "-v", "1.1"}, want: "invalid gain"},
		{name: "bad trigger", args: []string{"cpu.usage", "-t", "over:95"}, want: "invalid trigger"},
		{name: "bad notes", args: []string{"cpu.usage", "-n", "H4"}, want: "invalid note"},
		{name: "bad rhythm", args: []string{"cpu.usage", "-r", "rhythm:120:1/8:x_o"}, want: "invalid rhythm pattern"},
		{name: "bad BPM", args: []string{"cpu.usage", "-b", "0"}, want: "invalid BPM"},
		{name: "bare duration", args: []string{"cpu.usage", "-d", "100"}, want: "invalid duration"},
		{name: "bad ADSR fields", args: []string{"cpu.usage", "-a", "5ms,20ms,.8"}, want: "invalid ADSR"},
		{name: "bad ADSR sustain", args: []string{"cpu.usage", "-a", "5ms,20ms,2,50ms"}, want: "ADSR sustain"},
		{name: "bad swing", args: []string{"cpu.usage", "--swing", "49"}, want: "invalid swing"},
		{name: "bad filter", args: []string{"cpu.usage", "-f", "lp:0"}, want: "filter cutoff"},
		{name: "unknown filter", args: []string{"cpu.usage", "-f", "bp:1k"}, want: "unknown filter"},
		{name: "bad delay", args: []string{"cpu.usage", "-x", "delay:0ms,.2,.3"}, want: "greater than zero"},
		{name: "bad drive", args: []string{"cpu.usage", "-x", "drive:2"}, want: "drive amount"},
		{name: "unknown effect", args: []string{"cpu.usage", "-x", "reverb:.2"}, want: "unknown effect"},
		{name: "unsupported output", args: []string{"cpu.usage", "-o", "speaker"}, want: "only - is supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := cli.Parse(test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse(%q) error = %v, want substring %q", test.args, err, test.want)
			}
			if strings.HasPrefix(err.Error(), "stash:") {
				t.Fatalf("library error includes executable prefix: %v", err)
			}
		})
	}
}

func TestParseRejectsDuplicateSingletonOptions(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"-w": "sine", "--range": "0..1", "-v": ".2", "-t": "above:.5",
		"-n": "C4", "-r": "rhythm:120:1/8:x-", "-b": "120", "-d": "100ms",
		"-a": "5ms,20ms,.8,50ms", "--swing": "50", "-o": "-",
	}
	for option, value := range values {
		option, value := option, value
		t.Run(option, func(t *testing.T) {
			t.Parallel()
			_, err := cli.Parse([]string{"cpu.usage", option, value, option, value})
			if err == nil || !strings.Contains(err.Error(), "duplicate option "+option) {
				t.Fatalf("duplicate %s error = %v", option, err)
			}
		})
	}
}

func TestParseAllowsRepeatedModulationFilterAndEffect(t *testing.T) {
	t.Parallel()
	_, err := cli.Parse([]string{
		"cpu.usage",
		"-m", "freq=1..2", "-m", "gain=.1..1",
		"-f", "lp:1k", "-f", "hp:80",
		"-x", "drive:.2", "-x", "delay:1ms,.1,.2",
	})
	if err != nil {
		t.Fatal(err)
	}
}
