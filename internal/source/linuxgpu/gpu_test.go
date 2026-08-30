package linuxgpu

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const testDevicePath = "sys/class/drm/card2/device"

func completeGPUFS() fstest.MapFS {
	return fstest.MapFS{
		"sys/class/drm/card0/device/vendor":                      &fstest.MapFile{Data: []byte("0x10de\n")},
		testDevicePath + "/vendor":                               &fstest.MapFile{Data: []byte("0x1002\n")},
		testDevicePath + "/gpu_busy_percent":                     &fstest.MapFile{Data: []byte("67\n")},
		testDevicePath + "/mem_info_vram_total":                  &fstest.MapFile{Data: []byte("8589934592\n")},
		testDevicePath + "/mem_info_vram_used":                   &fstest.MapFile{Data: []byte("2147483648\n")},
		testDevicePath + "/hwmon/hwmon7/name":                    &fstest.MapFile{Data: []byte("amdgpu\n")},
		testDevicePath + "/hwmon/hwmon7/freq1_input":             &fstest.MapFile{Data: []byte("1500000000\n")},
		testDevicePath + "/hwmon/hwmon7/temp1_input":             &fstest.MapFile{Data: []byte("55125\n")},
		testDevicePath + "/hwmon/hwmon7/power1_average":          &fstest.MapFile{Data: []byte("125000000\n")},
		"sys/class/drm/renderD128/device/vendor":                 &fstest.MapFile{Data: []byte("0x1002\n")},
		"sys/class/drm/card2-DP-1/device/gpu_busy_percent":       &fstest.MapFile{Data: []byte("100\n")},
		"sys/class/drm/card2/device/hwmon/hwmon8/name":           &fstest.MapFile{Data: []byte("not-amdgpu\n")},
		"sys/class/drm/card2/device/hwmon/hwmon8/power1_average": &fstest.MapFile{Data: []byte("999999999\n")},
	}
}

func TestRegisterDetectsAMDGPUMetricsAndCollectsValues(t *testing.T) {
	t.Parallel()
	filesystem := completeGPUFS()
	registry := source.NewRegistry()
	when := time.Unix(1700, 42)
	if err := Register(context.Background(), registry, filesystem, func() time.Time { return when }); err != nil {
		t.Fatal(err)
	}

	wantNames := []string{FrequencyName, PowerName, TemperatureName, UsageName, VRAMName}
	entries := registry.List()
	gotNames := make([]string, len(entries))
	for index, entry := range entries {
		gotNames[index] = entry.Info.Name
		if !entry.Available {
			t.Fatalf("%s unexpectedly unavailable: %s", entry.Info.Name, entry.UnavailableReason)
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("registered names = %v, want %v", gotNames, wantNames)
	}

	wants := map[string]float64{
		UsageName: 67, FrequencyName: 1_500_000_000, TemperatureName: 55.125,
		PowerName: 125, VRAMName: 2_147_483_648,
	}
	for name, want := range wants {
		collector, err := registry.NewCollector(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		sample, err := collector.Collect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		scalar, ok := sample.(source.ScalarSample)
		if !ok || scalar.Time != when || math.Abs(scalar.Value-want) > 1e-9 {
			t.Fatalf("%s sample = %#v, want %v at %s", name, sample, want, when)
		}
	}

	usage, _ := registry.Lookup(UsageName)
	if usage.Info.Unit != "%" || usage.Info.NaturalMin == nil || *usage.Info.NaturalMin != 0 || usage.Info.NaturalMax == nil || *usage.Info.NaturalMax != 100 {
		t.Fatalf("usage metadata = %#v", usage.Info)
	}
	vram, _ := registry.Lookup(VRAMName)
	if vram.Info.Unit != "B" || vram.Info.NaturalMax == nil || *vram.Info.NaturalMax != 8_589_934_592 {
		t.Fatalf("VRAM metadata = %#v", vram.Info)
	}
}

func TestRegisterMissingGPURecordsEveryCanonicalSourceUnavailable(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	if err := Register(context.Background(), registry, fstest.MapFS{}, time.Now); err != nil {
		t.Fatal(err)
	}
	for _, definition := range sourceDefinitions {
		entry, ok := registry.Lookup(definition.name)
		if !ok || entry.Available || !strings.Contains(entry.UnavailableReason, "no supported DRM GPU backend") {
			t.Fatalf("entry %s = %#v, found %v", definition.name, entry, ok)
		}
	}
	vram, _ := registry.Lookup(VRAMName)
	if vram.Info.NaturalMin != nil || vram.Info.NaturalMax != nil {
		t.Fatalf("unprobed VRAM has fabricated capacity: %#v", vram.Info)
	}
}

func TestPartialOrMalformedMetricsStayUnavailable(t *testing.T) {
	t.Parallel()
	filesystem := fstest.MapFS{
		"sys/class/drm/card0/device/vendor":                    &fstest.MapFile{Data: []byte("0x1002\n")},
		"sys/class/drm/card0/device/gpu_busy_percent":          &fstest.MapFile{Data: []byte("101\n")},
		"sys/class/drm/card0/device/mem_info_vram_total":       &fstest.MapFile{Data: []byte("4096\n")},
		"sys/class/drm/card0/device/mem_info_vram_used":        &fstest.MapFile{Data: []byte("2048\n")},
		"sys/class/drm/card0/device/hwmon/hwmon0/name":         &fstest.MapFile{Data: []byte("amdgpu\n")},
		"sys/class/drm/card0/device/hwmon/hwmon0/temp1_input":  &fstest.MapFile{Data: []byte("warm\n")},
		"sys/class/drm/card0/device/hwmon/hwmon0/power1_input": &fstest.MapFile{Data: []byte("25000000\n")},
	}
	registry := source.NewRegistry()
	if err := Register(context.Background(), registry, filesystem, time.Now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{UsageName, FrequencyName, TemperatureName} {
		entry, _ := registry.Lookup(name)
		if entry.Available || entry.UnavailableReason == "" {
			t.Fatalf("malformed/missing metric %s = %#v", name, entry)
		}
	}
	for _, name := range []string{PowerName, VRAMName} {
		entry, _ := registry.Lookup(name)
		if !entry.Available {
			t.Fatalf("independent valid metric %s unavailable: %#v", name, entry)
		}
	}
}

func TestCollectorRejectsRuntimeCorruptionAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	filesystem := completeGPUFS()
	registry := source.NewRegistry()
	if err := Register(context.Background(), registry, filesystem, time.Now); err != nil {
		t.Fatal(err)
	}
	collector, err := registry.NewCollector(context.Background(), UsageName)
	if err != nil {
		t.Fatal(err)
	}
	filesystem[testDevicePath+"/gpu_busy_percent"].Data = []byte("busy\n")
	if sample, err := collector.Collect(context.Background()); sample != nil || err == nil || !strings.Contains(err.Error(), "invalid percent") {
		t.Fatalf("corrupt Collect = (%#v, %v)", sample, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sample, err := collector.Collect(ctx); sample != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Collect = (%#v, %v)", sample, err)
	}
}

func TestRegisterCancellationAndInputValidation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	registry := source.NewRegistry()
	if err := Register(ctx, registry, completeGPUFS(), time.Now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Register error = %v", err)
	}
	if entries := registry.List(); len(entries) != 0 {
		t.Fatalf("registered after cancellation: %#v", entries)
	}
	if err := Register(context.Background(), nil, completeGPUFS(), time.Now); err == nil {
		t.Fatal("Register accepted nil registry")
	}
	if err := Register(context.Background(), source.NewRegistry(), nil, time.Now); err == nil {
		t.Fatal("Register accepted nil filesystem")
	}
}

func TestMetricParsersRejectNonPhysicalValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		metric metricProbe
		data   string
	}{
		{"usage above range", metricProbe{path: "value", kind: metricUsage}, "101"},
		{"negative usage", metricProbe{path: "value", kind: metricUsage}, "-1"},
		{"zero frequency", metricProbe{path: "value", kind: metricFrequency}, "0"},
		{"bad temperature", metricProbe{path: "value", kind: metricTemperature}, "hot"},
		{"vram above total", metricProbe{path: "value", kind: metricVRAM, maximum: 10}, "11"},
		{"multiple fields", metricProbe{path: "value", kind: metricPower}, "1 2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := readMetric(fstest.MapFS{"value": &fstest.MapFile{Data: []byte(test.data)}}, test.metric); err == nil {
				t.Fatalf("readMetric(%q) unexpectedly succeeded", test.data)
			}
		})
	}
}

func TestMissingMetricErrorPreservesNotExist(t *testing.T) {
	t.Parallel()
	_, err := readMetric(fstest.MapFS{}, metricProbe{path: "missing", kind: metricUsage})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing metric error = %v", err)
	}
}
