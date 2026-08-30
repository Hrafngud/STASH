package linuxgpu

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zalmo/stash/internal/source"
)

type fakeNVMLClient struct {
	devices   []nvmlDevice
	countErr  error
	deviceErr error
}

func (client *fakeNVMLClient) DeviceCount() (uint32, error) {
	return uint32(len(client.devices)), client.countErr
}

func (client *fakeNVMLClient) Device(index uint32) (nvmlDevice, error) {
	if client.deviceErr != nil {
		return nil, client.deviceErr
	}
	if int(index) >= len(client.devices) {
		return nil, errors.New("device index is out of range")
	}
	return client.devices[index], nil
}

type fakeNVMLDevice struct {
	usage          uint32
	frequency      uint32
	temperature    uint32
	power          uint32
	used           uint64
	total          uint64
	usageErr       error
	frequencyErr   error
	temperatureErr error
	powerErr       error
	memoryErr      error
}

func completeNVMLFixture() *fakeNVMLDevice {
	return &fakeNVMLDevice{
		usage: 73, frequency: 1_365, temperature: 61, power: 83_750,
		used: 1_610_612_736, total: 6_442_450_944,
	}
}

func (device *fakeNVMLDevice) UsagePercent() (uint32, error) {
	return device.usage, device.usageErr
}

func (device *fakeNVMLDevice) GraphicsClockMHz() (uint32, error) {
	return device.frequency, device.frequencyErr
}

func (device *fakeNVMLDevice) TemperatureC() (uint32, error) {
	return device.temperature, device.temperatureErr
}

func (device *fakeNVMLDevice) PowerMilliwatts() (uint32, error) {
	return device.power, device.powerErr
}

func (device *fakeNVMLDevice) MemoryBytes() (uint64, uint64, error) {
	return device.used, device.total, device.memoryErr
}

func registerNVMLFixture(t *testing.T, filesystem fstest.MapFS, device *fakeNVMLDevice) *source.Registry {
	t.Helper()
	registry := source.NewRegistry()
	client := &fakeNVMLClient{devices: []nvmlDevice{device}}
	if err := registerWithNVML(context.Background(), registry, filesystem, func() time.Time { return time.Unix(1800, 0) }, func() (nvmlClient, error) {
		return client, nil
	}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestRegisterDetectsNVIDIANVMLMetricsAndCollectsValues(t *testing.T) {
	t.Parallel()
	device := completeNVMLFixture()
	registry := registerNVMLFixture(t, fstest.MapFS{}, device)
	wants := map[string]float64{
		UsageName: 73, FrequencyName: 1_365_000_000, TemperatureName: 61,
		PowerName: 83.75, VRAMName: 1_610_612_736,
	}
	for name, want := range wants {
		entry, ok := registry.Lookup(name)
		if !ok || !entry.Available {
			t.Fatalf("NVIDIA entry %s = %#v, found %v", name, entry, ok)
		}
		collector, err := registry.NewCollector(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		sample, err := collector.Collect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		scalar := sample.(source.ScalarSample)
		if math.Abs(scalar.Value-want) > 1e-9 || scalar.Time != time.Unix(1800, 0) {
			t.Fatalf("%s sample = %#v, want %v", name, scalar, want)
		}
	}
	vram, _ := registry.Lookup(VRAMName)
	if vram.Info.NaturalMin == nil || *vram.Info.NaturalMin != 0 || vram.Info.NaturalMax == nil || *vram.Info.NaturalMax != float64(device.total) {
		t.Fatalf("NVIDIA VRAM metadata = %#v", vram.Info)
	}
}

func TestMixedVendorSelectionPreservesAMDGPUWithoutLoadingNVML(t *testing.T) {
	t.Parallel()
	loads := 0
	registry := source.NewRegistry()
	err := registerWithNVML(context.Background(), registry, completeGPUFS(), time.Now, func() (nvmlClient, error) {
		loads++
		return &fakeNVMLClient{devices: []nvmlDevice{completeNVMLFixture()}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatalf("NVML loaded %d times despite selected AMDGPU backend", loads)
	}
	collector, err := registry.NewCollector(context.Background(), UsageName)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if err != nil || sample.(source.ScalarSample).Value != 67 {
		t.Fatalf("mixed-vendor usage = (%#v, %v), want AMDGPU value 67", sample, err)
	}
}

func TestNVIDIAMultipleDevicesSelectsIndexZero(t *testing.T) {
	t.Parallel()
	first, second := completeNVMLFixture(), completeNVMLFixture()
	first.usage, second.usage = 11, 99
	registry := source.NewRegistry()
	client := &fakeNVMLClient{devices: []nvmlDevice{first, second}}
	if err := registerWithNVML(context.Background(), registry, fstest.MapFS{}, time.Now, func() (nvmlClient, error) { return client, nil }); err != nil {
		t.Fatal(err)
	}
	collector, _ := registry.NewCollector(context.Background(), UsageName)
	sample, err := collector.Collect(context.Background())
	if err != nil || sample.(source.ScalarSample).Value != 11 {
		t.Fatalf("multi-device usage = (%#v, %v), want device zero value 11", sample, err)
	}
}

func TestMissingNVMLRecordsExplicitUnavailableReasons(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	err := registerWithNVML(context.Background(), registry, fstest.MapFS{}, time.Now, func() (nvmlClient, error) {
		return nil, errors.New("libnvidia-ml.so.1 not found")
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range sourceDefinitions {
		entry, ok := registry.Lookup(definition.name)
		if !ok || entry.Available || !strings.Contains(entry.UnavailableReason, "libnvidia-ml.so.1 not found") {
			t.Fatalf("missing-NVML entry %s = %#v, found %v", definition.name, entry, ok)
		}
	}
}

func TestNVIDIAPartialMetricsRemainIndependentlyUnavailable(t *testing.T) {
	t.Parallel()
	device := completeNVMLFixture()
	device.frequencyErr = errors.New("clock unsupported")
	device.powerErr = errors.New("permission denied")
	registry := registerNVMLFixture(t, fstest.MapFS{}, device)
	for _, name := range []string{FrequencyName, PowerName} {
		entry, _ := registry.Lookup(name)
		if entry.Available || entry.UnavailableReason == "" {
			t.Fatalf("partial NVIDIA metric %s = %#v", name, entry)
		}
	}
	for _, name := range []string{UsageName, TemperatureName, VRAMName} {
		entry, _ := registry.Lookup(name)
		if !entry.Available {
			t.Fatalf("independent NVIDIA metric %s unavailable: %#v", name, entry)
		}
	}
}

func TestNVIDIACollectorReportsDriverLossAndCancellation(t *testing.T) {
	t.Parallel()
	device := completeNVMLFixture()
	registry := registerNVMLFixture(t, fstest.MapFS{}, device)
	collector, err := registry.NewCollector(context.Background(), UsageName)
	if err != nil {
		t.Fatal(err)
	}
	device.usageErr = errors.New("GPU is lost")
	if sample, err := collector.Collect(context.Background()); sample != nil || err == nil || !strings.Contains(err.Error(), "GPU is lost") {
		t.Fatalf("driver-loss Collect = (%#v, %v)", sample, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sample, err := collector.Collect(ctx); sample != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled NVIDIA Collect = (%#v, %v)", sample, err)
	}
}

func TestNVIDIADetectionFailuresDoNotFabricateSources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		client nvmlClient
	}{
		{"device count failure", &fakeNVMLClient{countErr: errors.New("driver unavailable")}},
		{"no devices", &fakeNVMLClient{}},
		{"device open failure", &fakeNVMLClient{devices: []nvmlDevice{completeNVMLFixture()}, deviceErr: errors.New("permission denied")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := source.NewRegistry()
			if err := registerWithNVML(context.Background(), registry, fstest.MapFS{}, time.Now, func() (nvmlClient, error) { return test.client, nil }); err != nil {
				t.Fatal(err)
			}
			for _, entry := range registry.List() {
				if entry.Available || entry.UnavailableReason == "" {
					t.Fatalf("detection failure registered %#v", entry)
				}
			}
		})
	}
}
