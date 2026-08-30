package discovery_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/discovery"
	"github.com/zalmo/stash/internal/source"
)

type unusedCollector struct{}

func (unusedCollector) Collect(context.Context) (source.Sample, error) { return nil, io.EOF }

func discoveryRegistry(t *testing.T) *source.Registry {
	t.Helper()
	registry := source.NewRegistry()
	factory := func(context.Context) (source.Collector, error) { return unusedCollector{}, nil }
	register := func(info source.Info) {
		t.Helper()
		if err := registry.RegisterAvailable(info, factory); err != nil {
			t.Fatal(err)
		}
	}
	usageMinimum, usageMaximum := 0.0, 100.0
	register(source.Info{Name: "cpu.usage", Kind: source.KindScalar, Unit: "%", NaturalMin: &usageMinimum, NaturalMax: &usageMaximum})
	register(source.Info{Name: "cpu.core.10.usage", Kind: source.KindScalar, Unit: "%", NaturalMin: &usageMinimum, NaturalMax: &usageMaximum})
	register(source.Info{Name: "cpu.core.2.usage", Kind: source.KindScalar, Unit: "%", NaturalMin: &usageMinimum, NaturalMax: &usageMaximum})
	register(source.Info{Name: "cpu.cores.usage", Kind: source.KindVector, Unit: "%", NaturalMin: &usageMinimum, NaturalMax: &usageMaximum})
	frequencyMinimum, frequencyMaximum := 800_000_000.0, 4_200_000_000.0
	register(source.Info{Name: "cpu.freq", Kind: source.KindScalar, Unit: "Hz", NaturalMin: &frequencyMinimum, NaturalMax: &frequencyMaximum})
	register(source.Info{Name: "-", Kind: source.KindScalar, Unit: "unitless"})
	if err := registry.RegisterUnavailable(
		source.Info{Name: "cpu.temp", Kind: source.KindScalar, Unit: "°C"},
		"no reliable\nCPU sensor",
	); err != nil {
		t.Fatal(err)
	}
	return registry
}

func render(t *testing.T, registry *source.Registry, args ...string) (string, error) {
	t.Helper()
	command, err := cli.Parse(args)
	if err != nil {
		return "", err
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := discovery.Write(&output, registry, plan); err != nil {
		return output.String(), err
	}
	return output.String(), nil
}

func TestListSourcesGoldenAndPrefixFiltering(t *testing.T) {
	t.Parallel()
	registry := discoveryRegistry(t)
	output, err := render(t, registry, "-l")
	if err != nil {
		t.Fatal(err)
	}
	want := "" +
		"NAME\tKIND\tUNIT\tAVAILABILITY\n" +
		"-\tscalar\tunitless\tavailable\n" +
		"cpu.core.10.usage\tscalar\t%\tavailable\n" +
		"cpu.core.2.usage\tscalar\t%\tavailable\n" +
		"cpu.cores.usage\tvector\t%\tavailable\n" +
		"cpu.freq\tscalar\tHz\tavailable\n" +
		"cpu.temp\tscalar\t°C\tunavailable: no reliable CPU sensor\n" +
		"cpu.usage\tscalar\t%\tavailable\n"
	if output != want {
		t.Fatalf("stash -l output:\n%s\nwant:\n%s", output, want)
	}

	filtered, err := render(t, registry, "-l", "cpu")
	if err != nil {
		t.Fatal(err)
	}
	wantFiltered := "" +
		"NAME\tKIND\tUNIT\tAVAILABILITY\n" +
		"cpu.core.10.usage\tscalar\t%\tavailable\n" +
		"cpu.core.2.usage\tscalar\t%\tavailable\n" +
		"cpu.cores.usage\tvector\t%\tavailable\n" +
		"cpu.freq\tscalar\tHz\tavailable\n" +
		"cpu.temp\tscalar\t°C\tunavailable: no reliable CPU sensor\n" +
		"cpu.usage\tscalar\t%\tavailable\n"
	if filtered != wantFiltered {
		t.Fatalf("stash -l cpu output:\n%s\nwant:\n%s", filtered, wantFiltered)
	}
}

func TestInspectSourceGolden(t *testing.T) {
	t.Parallel()
	registry := discoveryRegistry(t)
	output, err := render(t, registry, "-i", "cpu.usage")
	if err != nil {
		t.Fatal(err)
	}
	want := "name: cpu.usage\nkind: scalar\nunit: %\nnatural range: 0..100\navailability: available\n"
	if output != want {
		t.Fatalf("inspection output:\n%s\nwant:\n%s", output, want)
	}

	unavailable, err := render(t, registry, "-i", "cpu.temp")
	if err != nil {
		t.Fatal(err)
	}
	wantUnavailable := "name: cpu.temp\nkind: scalar\nunit: °C\nnatural range: unspecified\navailability: unavailable: no reliable CPU sensor\n"
	if unavailable != wantUnavailable {
		t.Fatalf("unavailable inspection output:\n%s\nwant:\n%s", unavailable, wantUnavailable)
	}
}

func TestResolveEveryDocumentedPrimitiveForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argument string
		want     []string
	}{
		{argument: "C4", want: []string{"kind: note\n", "count: 1\n", "  0: C4 (261.6255653005986 Hz)\n"}},
		{argument: "scale:C4:major:8", want: []string{"kind: scale\n", "count: 8\n", "  7: C5 (523.2511306011972 Hz)\n"}},
		{argument: "mode:E3:phrygian:8", want: []string{"kind: mode\n", "count: 8\n", "  7: E4 (329.6275569128699 Hz)\n"}},
		{argument: "rhythm:120:1/8:x-x-x-x-", want: []string{"kind: rhythm\n", "bpm: 120\n", "division: 1/8\n", "pattern: x-x-x-x-\n", "steps: 8\n", "controls: rhythm.gate,rhythm.hit,rhythm.step,rhythm.velocity,rhythm.phase\n"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.argument, func(t *testing.T) {
			t.Parallel()
			output, err := render(t, nil, "-p", test.argument)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(output, "primitive: "+test.argument+"\n") {
				t.Fatalf("primitive output missing input label:\n%s", output)
			}
			for _, fragment := range test.want {
				if !strings.Contains(output, fragment) {
					t.Errorf("primitive output missing %q:\n%s", fragment, output)
				}
			}
		})
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestWriteValidatesModeDependenciesAndOutput(t *testing.T) {
	t.Parallel()
	registry := discoveryRegistry(t)
	listCommand, err := cli.Parse([]string{"-l"})
	if err != nil {
		t.Fatal(err)
	}
	listPlan, err := cli.BuildPlan(listCommand, registry)
	if err != nil {
		t.Fatal(err)
	}

	if err := discovery.Write(nil, registry, listPlan); err == nil || !strings.Contains(err.Error(), "output is nil") {
		t.Fatalf("nil output error = %v", err)
	}
	var output bytes.Buffer
	if err := discovery.Write(&output, nil, listPlan); err == nil || !strings.Contains(err.Error(), "registry is nil") {
		t.Fatalf("nil registry error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("validation failure wrote %q", output.String())
	}
	if err := discovery.Write(shortWriter{}, registry, listPlan); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v, want io.ErrShortWrite", err)
	}
	if err := discovery.Write(io.Discard, registry, cli.Plan{Mode: cli.ModeTelemetry}); err == nil || !strings.Contains(err.Error(), "not a discovery mode") {
		t.Fatalf("telemetry mode error = %v", err)
	}
	if err := discovery.Write(io.Discard, registry, cli.Plan{Mode: cli.ModeInspect}); err == nil || !strings.Contains(err.Error(), "no source metadata") {
		t.Fatalf("empty inspect plan error = %v", err)
	}
	if err := discovery.Write(io.Discard, registry, cli.Plan{Mode: cli.ModePrimitive, Command: cli.Command{Primitive: "C4"}}); err == nil || !strings.Contains(err.Error(), "resolution is empty") {
		t.Fatalf("empty primitive plan error = %v", err)
	}
}
