package linuxproc

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/discovery"
	"github.com/zalmo/stash/internal/source"
)

type sequenceReader struct {
	observations [][]byte
	index        int
}

func (reader *sequenceReader) read(context.Context) ([]byte, error) {
	if reader.index >= len(reader.observations) {
		return nil, errors.New("fixture sequence exhausted")
	}
	data := reader.observations[reader.index]
	reader.index++
	return data, nil
}

type sequenceClock struct {
	times []time.Time
	index int
}

func (clock *sequenceClock) now() time.Time {
	value := clock.times[clock.index]
	clock.index++
	return value
}

func staticReader(data []byte) ReadFile {
	return func(context.Context) ([]byte, error) { return data, nil }
}

func TestRegisterSourcesAreStableRegisteredAndInspectable(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	if err := Register(context.Background(), registry,
		staticReader(fixture(t, "meminfo_good.txt")),
		staticReader(fixture(t, "netdev_base.txt")),
		staticReader(fixture(t, "diskstats_base.txt")),
		time.Now,
	); err != nil {
		t.Fatal(err)
	}

	entries := registry.List()
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Info.Name
		if !entry.Available || entry.Info.Kind != source.KindScalar {
			t.Fatalf("entry = %#v", entry)
		}
	}
	want := []string{
		"io.loop0.ops", "io.loop0.read", "io.loop0.write",
		"io.nvme0n1.ops", "io.nvme0n1.read", "io.nvme0n1.write",
		"net.enp4s0.rx", "net.enp4s0.rx.packets", "net.enp4s0.tx", "net.enp4s0.tx.packets",
		"net.lo.rx", "net.lo.rx.packets", "net.lo.tx", "net.lo.tx.packets",
		"ram.free", "ram.used",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for name, unit := range map[string]string{"ram.used": "B", "net.enp4s0.rx": "B/s", "net.enp4s0.rx.packets": "packets/s", "io.nvme0n1.ops": "ops/s"} {
		entry, ok := registry.Lookup(name)
		if !ok || entry.Info.Unit != unit {
			t.Fatalf("entry %s = %#v, %v", name, entry, ok)
		}
	}

	command, err := cli.Parse([]string{"-i", "io.nvme0n1.read"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := discovery.Write(&output, registry, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "name: io.nvme0n1.read\n") || !strings.Contains(output.String(), "unit: B/s\n") || !strings.Contains(output.String(), "availability: available\n") {
		t.Fatalf("inspection output:\n%s", output.String())
	}
}

func TestMemoryNetworkAndDiskCollection(t *testing.T) {
	t.Parallel()
	when := time.Unix(100, 0)
	memory := &memoryCollector{read: staticReader(fixture(t, "meminfo_good.txt")), now: func() time.Time { return when }, metric: memoryUsed}
	sample, err := memory.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantUsed := float64((16384000 - 4096000) * 1024)
	if scalar := sample.(source.ScalarSample); scalar.Value != wantUsed || scalar.Time != when {
		t.Fatalf("memory sample = %#v", scalar)
	}

	for _, test := range []struct {
		name   string
		metric networkMetric
		want   float64
	}{{"rx bytes", networkReceiveBytes, 500}, {"tx bytes", networkTransmitBytes, 600}, {"rx packets", networkReceivePackets, 5}, {"tx packets", networkTransmitPackets, 6}} {
		t.Run(test.name, func(t *testing.T) {
			reader := &sequenceReader{observations: [][]byte{fixture(t, "netdev_base.txt"), fixture(t, "netdev_next.txt")}}
			clock := &sequenceClock{times: []time.Time{when, when.Add(2 * time.Second)}}
			collector, err := newNetworkCollector(context.Background(), reader.read, clock.now, "enp4s0", test.metric)
			if err != nil {
				t.Fatal(err)
			}
			sample, err := collector.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if value := sample.(source.ScalarSample).Value; value != test.want {
				t.Fatalf("rate = %v, want %v", value, test.want)
			}
		})
	}

	for _, test := range []struct {
		name   string
		metric diskMetric
		want   float64
	}{{"read", diskRead, 10240}, {"write", diskWrite, 25600}, {"operations", diskOperations, 5}} {
		t.Run(test.name, func(t *testing.T) {
			reader := &sequenceReader{observations: [][]byte{fixture(t, "diskstats_base.txt"), fixture(t, "diskstats_next.txt")}}
			clock := &sequenceClock{times: []time.Time{when, when.Add(2 * time.Second)}}
			collector, err := newDiskCollector(context.Background(), reader.read, clock.now, "nvme0n1", test.metric)
			if err != nil {
				t.Fatal(err)
			}
			sample, err := collector.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if value := sample.(source.ScalarSample).Value; value != test.want {
				t.Fatalf("rate = %v, want %v", value, test.want)
			}
		})
	}
}

func TestRateCollectorsResetAndRebaseline(t *testing.T) {
	t.Parallel()
	when := time.Unix(200, 0)
	reader := &sequenceReader{observations: [][]byte{fixture(t, "netdev_base.txt"), fixture(t, "netdev_reset.txt"), fixture(t, "netdev_after_reset.txt")}}
	clock := &sequenceClock{times: []time.Time{when, when.Add(time.Second), when.Add(2 * time.Second)}}
	collector, err := newNetworkCollector(context.Background(), reader.read, clock.now, "enp4s0", networkReceiveBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Collect(context.Background()); !errors.Is(err, ErrCounterReset) {
		t.Fatalf("reset error = %v", err)
	}
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value := sample.(source.ScalarSample).Value; value != 200 {
		t.Fatalf("post-reset rate = %v, want 200", value)
	}

	diskReader := &sequenceReader{observations: [][]byte{fixture(t, "diskstats_base.txt"), fixture(t, "diskstats_reset.txt"), fixture(t, "diskstats_after_reset.txt")}}
	diskClock := &sequenceClock{times: []time.Time{when, when.Add(time.Second), when.Add(2 * time.Second)}}
	disk, err := newDiskCollector(context.Background(), diskReader.read, diskClock.now, "nvme0n1", diskOperations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.Collect(context.Background()); !errors.Is(err, ErrCounterReset) {
		t.Fatalf("disk reset error = %v", err)
	}
	sample, err = disk.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value := sample.(source.ScalarSample).Value; value != 5 {
		t.Fatalf("post-reset disk ops = %v, want 5", value)
	}
}

func TestRegisterUnavailableAndCancellation(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	if err := Register(context.Background(), registry,
		staticReader(fixture(t, "meminfo_unavailable.txt")),
		staticReader(fixture(t, "netdev_unavailable.txt")),
		staticReader(fixture(t, "diskstats_unavailable.txt")),
		time.Now,
	); err != nil {
		t.Fatal(err)
	}
	if entries := registry.List(); len(entries) != 2 {
		t.Fatalf("unavailable entries = %#v", entries)
	}
	for _, name := range []string{RAMUsedName, RAMFreeName} {
		entry, ok := registry.Lookup(name)
		if !ok || entry.Available || !strings.Contains(entry.UnavailableReason, "MemAvailable is missing") {
			t.Fatalf("entry %s = %#v, %v", name, entry, ok)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Register(ctx, source.NewRegistry(), ReadMemInfo, ReadNetDev, ReadDiskStats, time.Now); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
