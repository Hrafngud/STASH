package source_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/source"
)

type fixedCollector struct{}

func (fixedCollector) Collect(context.Context) (source.Sample, error) {
	return source.ScalarSample{Value: 12.5, Time: time.Unix(1, 0)}, nil
}

func scalarInfo(name string) source.Info {
	minimum, maximum := 0.0, 100.0
	return source.Info{Name: name, Kind: source.KindScalar, Unit: "%", NaturalMin: &minimum, NaturalMax: &maximum}
}

func TestSampleContractsAndDefaultRate(t *testing.T) {
	t.Parallel()
	when := time.Unix(123, 456)
	scalar := source.ScalarSample{Value: 1, Time: when}
	vector := source.VectorSample{Values: []float64{1, 2}, Time: when}
	if scalar.SampleKind() != source.KindScalar || scalar.SampleTime() != when {
		t.Fatalf("scalar sample contract mismatch: %#v", scalar)
	}
	if vector.SampleKind() != source.KindVector || vector.SampleTime() != when {
		t.Fatalf("vector sample contract mismatch: %#v", vector)
	}
	if source.DefaultSampleInterval != time.Second/20 {
		t.Fatalf("default interval = %s, want 20 Hz", source.DefaultSampleInterval)
	}
}

func TestRegistryListsStableEntriesAndConstructsCollector(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	factory := func(context.Context) (source.Collector, error) { return fixedCollector{}, nil }
	if err := registry.RegisterAvailable(scalarInfo("cpu.usage"), factory); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterUnavailable(scalarInfo("cpu.temp"), "no reliable sensor"); err != nil {
		t.Fatal(err)
	}

	entries := registry.List()
	gotNames := []string{entries[0].Info.Name, entries[1].Info.Name}
	wantNames := []string{"cpu.temp", "cpu.usage"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("List names = %v, want %v", gotNames, wantNames)
	}
	collector, err := registry.NewCollector(context.Background(), "cpu.usage")
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := sample.(source.ScalarSample).Value; got != 12.5 {
		t.Fatalf("sample value = %v, want 12.5", got)
	}

	_, err = registry.NewCollector(context.Background(), "cpu.temp")
	var unavailable *source.UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != "no reliable sensor" {
		t.Fatalf("unavailable error = %v", err)
	}
	if _, err := registry.NewCollector(context.Background(), "cpu.missing"); err == nil || !strings.Contains(err.Error(), "unknown source") {
		t.Fatalf("unknown-source error = %v", err)
	}
}

func TestRegistryRejectsInvalidAndDuplicateDefinitions(t *testing.T) {
	t.Parallel()
	factory := func(context.Context) (source.Collector, error) { return fixedCollector{}, nil }
	tests := []struct {
		name string
		info source.Info
	}{
		{name: "empty name", info: source.Info{Kind: source.KindScalar, Unit: "%"}},
		{name: "bad kind", info: source.Info{Name: "cpu.usage", Kind: "matrix", Unit: "%"}},
		{name: "empty unit", info: source.Info{Name: "cpu.usage", Kind: source.KindScalar}},
		{name: "half range", info: func() source.Info {
			value := 0.0
			return source.Info{Name: "cpu.usage", Kind: source.KindScalar, Unit: "%", NaturalMin: &value}
		}()},
		{name: "reversed range", info: func() source.Info {
			low, high := 2.0, 1.0
			return source.Info{Name: "cpu.usage", Kind: source.KindScalar, Unit: "%", NaturalMin: &low, NaturalMax: &high}
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := source.NewRegistry()
			if err := registry.RegisterAvailable(test.info, factory); err == nil {
				t.Fatalf("RegisterAvailable(%#v) unexpectedly succeeded", test.info)
			}
		})
	}

	registry := source.NewRegistry()
	if err := registry.RegisterAvailable(scalarInfo("cpu.usage"), factory); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterAvailable(scalarInfo("cpu.usage"), factory); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate error = %v", err)
	}
	if err := registry.RegisterAvailable(scalarInfo("cpu.other"), nil); err == nil {
		t.Fatal("nil factory unexpectedly succeeded")
	}
	if err := registry.RegisterUnavailable(scalarInfo("cpu.temp"), "  "); err == nil {
		t.Fatal("empty unavailable reason unexpectedly succeeded")
	}
}
