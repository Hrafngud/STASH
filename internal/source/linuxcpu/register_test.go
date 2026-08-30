package linuxcpu

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/source"
)

func TestRegisterUsageRegistersMetadataAndStableCoreNames(t *testing.T) {
	t.Parallel()
	reader := &sequenceReader{observations: [][]byte{
		fixture(t, "proc_stat_base.txt"), // detection
		fixture(t, "proc_stat_base.txt"), // collector baseline
		fixture(t, "proc_stat_next.txt"), // collector sample
	}}
	registry := source.NewRegistry()
	when := time.Unix(500, 0)
	if err := RegisterUsage(context.Background(), registry, reader.read, func() time.Time { return when }); err != nil {
		t.Fatal(err)
	}

	entries := registry.List()
	gotNames := make([]string, len(entries))
	for index, entry := range entries {
		gotNames[index] = entry.Info.Name
		if !entry.Available {
			t.Fatalf("entry %s unexpectedly unavailable: %s", entry.Info.Name, entry.UnavailableReason)
		}
		if entry.Info.Unit != "%" || entry.Info.NaturalMin == nil || *entry.Info.NaturalMin != 0 || entry.Info.NaturalMax == nil || *entry.Info.NaturalMax != 100 {
			t.Fatalf("entry %s metadata = %#v", entry.Info.Name, entry.Info)
		}
	}
	wantNames := []string{
		"cpu.core.0.usage",
		"cpu.core.1.usage",
		"cpu.core.2.usage",
		"cpu.cores.usage",
		"cpu.usage",
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("registered names = %v, want %v", gotNames, wantNames)
	}
	if entry, _ := registry.Lookup(AggregateUsageName); entry.Info.Kind != source.KindScalar {
		t.Fatalf("aggregate kind = %q", entry.Info.Kind)
	}
	if entry, _ := registry.Lookup(CoresUsageName); entry.Info.Kind != source.KindVector {
		t.Fatalf("cores kind = %q", entry.Info.Kind)
	}

	collector, err := registry.NewCollector(context.Background(), CoresUsageName)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vector := sample.(source.VectorSample)
	if !reflect.DeepEqual(vector.Values, []float64{20, 50, 100}) || vector.Time != when {
		t.Fatalf("vector sample = %#v", vector)
	}
}

func TestRegisterUsageRecordsUnavailableProbe(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	read := func(context.Context) ([]byte, error) {
		return fixture(t, "proc_stat_malformed.txt"), nil
	}
	if err := RegisterUsage(context.Background(), registry, read, time.Now); err != nil {
		t.Fatal(err)
	}
	entries := registry.List()
	if got, want := len(entries), 2; got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
	for _, name := range []string{AggregateUsageName, CoresUsageName} {
		entry, ok := registry.Lookup(name)
		if !ok || entry.Available || !strings.Contains(entry.UnavailableReason, "invalid counter") {
			t.Fatalf("entry %s = %#v, %v", name, entry, ok)
		}
		_, err := registry.NewCollector(context.Background(), name)
		var unavailable *source.UnavailableError
		if !errors.As(err, &unavailable) || !strings.Contains(unavailable.Reason, "invalid counter") {
			t.Fatalf("NewCollector(%s) error = %v", name, err)
		}
	}
}

func TestRegisterUsageRecordsReadFailureAsUnavailable(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	read := func(context.Context) ([]byte, error) { return nil, errors.New("procfs is not mounted") }
	if err := RegisterUsage(context.Background(), registry, read, time.Now); err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Lookup(AggregateUsageName)
	if !ok || entry.Available || entry.UnavailableReason != "procfs is not mounted" {
		t.Fatalf("aggregate entry = %#v, %v", entry, ok)
	}
}

func TestRegisterUsagePropagatesCancellationWithoutRegistering(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RegisterUsage(ctx, registry, ReadProcStat, time.Now); !errors.Is(err, context.Canceled) {
		t.Fatalf("RegisterUsage error = %v, want context.Canceled", err)
	}
	if entries := registry.List(); len(entries) != 0 {
		t.Fatalf("registered entries after cancellation: %#v", entries)
	}
}
