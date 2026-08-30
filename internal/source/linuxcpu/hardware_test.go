package linuxcpu

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zalmo/stash/internal/source"
)

func hardwareFixture(name string) fs.FS {
	return os.DirFS("testdata/" + name)
}

func TestDetectFrequencyFromSysfsFixture(t *testing.T) {
	t.Parallel()
	probe, err := detectFrequency(context.Background(), hardwareFixture("hardware_good"))
	if err != nil {
		t.Fatal(err)
	}
	indices := make([]int, len(probe.cores))
	paths := make([]string, len(probe.cores))
	for index, core := range probe.cores {
		indices[index] = core.index
		paths[index] = core.path
		if core.minimumHz == nil || core.maximumHz == nil {
			t.Fatalf("core %d range is missing", core.index)
		}
	}
	if want := []int{0, 2, 10}; !reflect.DeepEqual(indices, want) {
		t.Fatalf("frequency core order = %v, want %v", indices, want)
	}
	if !strings.HasSuffix(paths[0], "cpuinfo_cur_freq") {
		t.Fatalf("core 0 path = %q, want cpuinfo_cur_freq preference", paths[0])
	}
	if !strings.HasSuffix(paths[1], "scaling_cur_freq") {
		t.Fatalf("core 2 path = %q, want scaling_cur_freq fallback", paths[1])
	}
	minimum, maximum := aggregateFrequencyRange(probe.cores)
	if minimum == nil || maximum == nil || *minimum != 800_000_000 || math.Abs(*maximum-4_266_666_666.666667) > 1e-6 {
		t.Fatalf("aggregate frequency range = %v..%v", pointerValue(minimum), pointerValue(maximum))
	}
	minimum, maximum = vectorFrequencyRange(probe.cores)
	if minimum == nil || maximum == nil || *minimum != 600_000_000 || *maximum != 5_000_000_000 {
		t.Fatalf("vector frequency range = %v..%v", pointerValue(minimum), pointerValue(maximum))
	}
}

func TestCoreFrequencyFallsBackFromUnreadablePreferredValue(t *testing.T) {
	t.Parallel()
	filesystem := fstest.MapFS{
		sysCPUPath + "/cpu0/cpufreq/cpuinfo_cur_freq": &fstest.MapFile{Data: []byte("unknown\n")},
		sysCPUPath + "/cpu0/cpufreq/scaling_cur_freq": &fstest.MapFile{Data: []byte("1500000\n")},
	}
	core, found, err := detectCoreFrequency(filesystem, 0)
	if err != nil || !found {
		t.Fatalf("detectCoreFrequency = (%#v, %v, %v)", core, found, err)
	}
	if !strings.HasSuffix(core.path, "scaling_cur_freq") {
		t.Fatalf("fallback path = %q", core.path)
	}
}

func TestRegisterHardwareAndCollectFixtureValues(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	when := time.Unix(1700, 123)
	if err := RegisterHardware(context.Background(), registry, hardwareFixture("hardware_good"), func() time.Time { return when }); err != nil {
		t.Fatal(err)
	}

	wantNames := []string{
		"cpu.core.0.freq",
		"cpu.core.10.freq",
		"cpu.core.2.freq",
		"cpu.cores.freq",
		"cpu.freq",
		"cpu.power",
		"cpu.temp",
	}
	entries := registry.List()
	gotNames := make([]string, len(entries))
	for index, entry := range entries {
		gotNames[index] = entry.Info.Name
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("registered names = %v, want %v", gotNames, wantNames)
	}

	assertScalarSample(t, registry, AggregateFrequencyName, (2_200_000_000+1_800_000_000+3_000_000_000)/3.0, when)
	assertScalarSample(t, registry, "cpu.core.2.freq", 1_800_000_000, when)
	assertScalarSample(t, registry, TemperatureName, 57.5, when)

	collector, err := registry.NewCollector(context.Background(), CoresFrequencyName)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vector, ok := sample.(source.VectorSample)
	if !ok || vector.Time != when || !reflect.DeepEqual(vector.Values, []float64{2_200_000_000, 1_800_000_000, 3_000_000_000}) {
		t.Fatalf("frequency vector = %#v", sample)
	}

	frequencyEntry, _ := registry.Lookup(AggregateFrequencyName)
	if frequencyEntry.Info.Unit != "Hz" || frequencyEntry.Info.NaturalMin == nil || *frequencyEntry.Info.NaturalMin != 800_000_000 || frequencyEntry.Info.NaturalMax == nil || math.Abs(*frequencyEntry.Info.NaturalMax-4_266_666_666.666667) > 1e-6 {
		t.Fatalf("aggregate frequency metadata = %#v", frequencyEntry.Info)
	}
	vectorEntry, _ := registry.Lookup(CoresFrequencyName)
	if vectorEntry.Info.NaturalMin == nil || *vectorEntry.Info.NaturalMin != 600_000_000 || vectorEntry.Info.NaturalMax == nil || *vectorEntry.Info.NaturalMax != 5_000_000_000 {
		t.Fatalf("vector frequency metadata = %#v", vectorEntry.Info)
	}
	temperatureEntry, _ := registry.Lookup(TemperatureName)
	if temperatureEntry.Info.Unit != "°C" || temperatureEntry.Info.NaturalMin != nil || temperatureEntry.Info.NaturalMax != nil {
		t.Fatalf("temperature metadata = %#v", temperatureEntry.Info)
	}
	powerEntry, _ := registry.Lookup(PowerName)
	if powerEntry.Available || powerEntry.Info.Unit != "W" || powerEntry.UnavailableReason != unavailablePowerReason {
		t.Fatalf("power entry = %#v", powerEntry)
	}
	_, err = registry.NewCollector(context.Background(), PowerName)
	var unavailable *source.UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != unavailablePowerReason {
		t.Fatalf("power collector error = %v", err)
	}
}

func TestTemperatureFallsBackToReliableThermalZone(t *testing.T) {
	t.Parallel()
	probe, err := detectTemperature(context.Background(), hardwareFixture("hardware_thermal"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sys/class/thermal/thermal_zone1/temp"}; !reflect.DeepEqual(probe.paths, want) {
		t.Fatalf("temperature paths = %v, want %v", probe.paths, want)
	}
	collector := temperatureCollector{filesystem: hardwareFixture("hardware_thermal"), paths: probe.paths, now: time.Now}
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value := sample.(source.ScalarSample).Value; value != 42.125 {
		t.Fatalf("thermal temperature = %v, want 42.125", value)
	}
}

func TestRegisterHardwareRecordsExplicitUnavailableStates(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	if err := RegisterHardware(context.Background(), registry, hardwareFixture("hardware_unavailable"), time.Now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{AggregateFrequencyName, CoresFrequencyName, TemperatureName, PowerName} {
		entry, ok := registry.Lookup(name)
		if !ok || entry.Available || entry.UnavailableReason == "" {
			t.Fatalf("unavailable entry %s = %#v, found %v", name, entry, ok)
		}
	}
	if _, ok := registry.Lookup("cpu.core.0.freq"); ok {
		t.Fatal("undiscoverable per-core frequency was registered")
	}
}

func TestCPUPowerUsesPackageEnergyDeltasAndWraps(t *testing.T) {
	t.Parallel()
	const zone0 = sysPowercapPath + "/intel-rapl:0"
	const zone1 = sysPowercapPath + "/intel-rapl:1"
	filesystem := fstest.MapFS{
		zone0 + "/name":                                         &fstest.MapFile{Data: []byte("package-0\n")},
		zone0 + "/energy_uj":                                    &fstest.MapFile{Data: []byte("9000000\n")},
		zone0 + "/max_energy_range_uj":                          &fstest.MapFile{Data: []byte("10000000\n")},
		zone1 + "/name":                                         &fstest.MapFile{Data: []byte("package-1\n")},
		zone1 + "/energy_uj":                                    &fstest.MapFile{Data: []byte("2000000\n")},
		zone1 + "/max_energy_range_uj":                          &fstest.MapFile{Data: []byte("10000000\n")},
		sysPowercapPath + "/intel-rapl:0:0/name":                &fstest.MapFile{Data: []byte("core\n")},
		sysPowercapPath + "/intel-rapl:0:0/energy_uj":           &fstest.MapFile{Data: []byte("9999999\n")},
		sysPowercapPath + "/intel-rapl:0:0/max_energy_range_uj": &fstest.MapFile{Data: []byte("10000000\n")},
	}
	start := time.Unix(100, 0)
	times := []time.Time{start, start.Add(2 * time.Second)}
	nextTime := func() time.Time {
		when := times[0]
		times = times[1:]
		return when
	}
	registry := source.NewRegistry()
	if err := RegisterHardware(context.Background(), registry, filesystem, nextTime); err != nil {
		t.Fatal(err)
	}
	entry, _ := registry.Lookup(PowerName)
	if !entry.Available || entry.Info.Unit != "W" {
		t.Fatalf("power entry = %#v", entry)
	}
	collector, err := registry.NewCollector(context.Background(), PowerName)
	if err != nil {
		t.Fatal(err)
	}
	filesystem[zone0+"/energy_uj"].Data = []byte("1000000\n")
	filesystem[zone1+"/energy_uj"].Data = []byte("6000000\n")
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scalar := sample.(source.ScalarSample)
	// zone0 wrapped for 2 J and zone1 advanced 4 J over two seconds.
	if scalar.Value != 3 || scalar.Time != start.Add(2*time.Second) {
		t.Fatalf("power sample = %#v, want 3 W", scalar)
	}
}

func TestCPUPowerMalformedAndRuntimeFailuresNeverFabricateSamples(t *testing.T) {
	t.Parallel()
	const zone = sysPowercapPath + "/intel-rapl:0"
	malformed := fstest.MapFS{
		zone + "/name":                &fstest.MapFile{Data: []byte("package-0\n")},
		zone + "/energy_uj":           &fstest.MapFile{Data: []byte("unknown\n")},
		zone + "/max_energy_range_uj": &fstest.MapFile{Data: []byte("10000000\n")},
	}
	registry := source.NewRegistry()
	if err := RegisterHardware(context.Background(), registry, malformed, time.Now); err != nil {
		t.Fatal(err)
	}
	entry, _ := registry.Lookup(PowerName)
	if entry.Available || !strings.Contains(entry.UnavailableReason, "invalid microjoule") {
		t.Fatalf("malformed power entry = %#v", entry)
	}

	valid := fstest.MapFS{
		zone + "/name":                &fstest.MapFile{Data: []byte("package-0\n")},
		zone + "/energy_uj":           &fstest.MapFile{Data: []byte("100\n")},
		zone + "/max_energy_range_uj": &fstest.MapFile{Data: []byte("1000\n")},
	}
	times := []time.Time{time.Unix(1, 0), time.Unix(2, 0)}
	now := func() time.Time { when := times[0]; times = times[1:]; return when }
	registry = source.NewRegistry()
	if err := RegisterHardware(context.Background(), registry, valid, now); err != nil {
		t.Fatal(err)
	}
	collector, err := registry.NewCollector(context.Background(), PowerName)
	if err != nil {
		t.Fatal(err)
	}
	valid[zone+"/energy_uj"].Data = []byte("1001\n")
	if sample, err := collector.Collect(context.Background()); sample != nil || err == nil || !strings.Contains(err.Error(), "exceeds range") {
		t.Fatalf("out-of-range power Collect = (%#v, %v)", sample, err)
	}
}

func TestMalformedHardwareDataNeverProducesSources(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	if err := RegisterHardware(context.Background(), registry, hardwareFixture("hardware_malformed"), time.Now); err != nil {
		t.Fatal(err)
	}
	frequency, _ := registry.Lookup(AggregateFrequencyName)
	if frequency.Available || !strings.Contains(frequency.UnavailableReason, "invalid kHz value") {
		t.Fatalf("malformed frequency entry = %#v", frequency)
	}
	temperature, _ := registry.Lookup(TemperatureName)
	if temperature.Available || !strings.Contains(temperature.UnavailableReason, "invalid millidegree Celsius") {
		t.Fatalf("malformed temperature entry = %#v", temperature)
	}
}

func TestHardwareCollectorsHonorCancellationAndRuntimeErrors(t *testing.T) {
	t.Parallel()
	filesystem := fstest.MapFS{
		"frequency":   &fstest.MapFile{Data: []byte("bad\n")},
		"temperature": &fstest.MapFile{Data: []byte("55000\n")},
	}
	frequency := frequencyCollector{
		filesystem: filesystem,
		cores:      []frequencyCore{{index: 0, path: "frequency"}},
		selected:   selector{kind: selectAggregate},
		now:        time.Now,
	}
	if sample, err := frequency.Collect(context.Background()); sample != nil || err == nil || !strings.Contains(err.Error(), "invalid kHz value") {
		t.Fatalf("malformed frequency Collect = (%#v, %v)", sample, err)
	}

	temperature := temperatureCollector{filesystem: filesystem, paths: []string{"temperature"}, now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sample, err := temperature.Collect(ctx); sample != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled temperature Collect = (%#v, %v)", sample, err)
	}
}

func TestHardwareParsersRejectMalformedAndNonPhysicalValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "1 2", "-1", "0", "1.5", "18446744073709551615"} {
		if _, err := parseKHz([]byte(value)); err == nil {
			t.Errorf("parseKHz(%q) unexpectedly succeeded", value)
		}
	}
	for _, value := range []string{"", "1 2", "warm", "-274000", "1000001"} {
		if _, err := parseMilliCelsius([]byte(value)); err == nil {
			t.Errorf("parseMilliCelsius(%q) unexpectedly succeeded", value)
		}
	}
	if value, err := parseKHz([]byte("2200000\n")); err != nil || value != 2_200_000_000 {
		t.Fatalf("parseKHz valid = (%v, %v)", value, err)
	}
	if value, err := parseMilliCelsius([]byte("-1250\n")); err != nil || math.Abs(value+1.25) > 1e-12 {
		t.Fatalf("parseMilliCelsius valid = (%v, %v)", value, err)
	}
}

func TestRegisterHardwarePropagatesCancellationWithoutRegistration(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RegisterHardware(ctx, registry, hardwareFixture("hardware_good"), time.Now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RegisterHardware error = %v, want context.Canceled", err)
	}
	if entries := registry.List(); len(entries) != 0 {
		t.Fatalf("registered entries after cancellation: %#v", entries)
	}
}

func TestHardwareCollectorFactoriesHonorCancellation(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	if err := RegisterHardware(context.Background(), registry, hardwareFixture("hardware_good"), time.Now); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, name := range []string{AggregateFrequencyName, TemperatureName} {
		if _, err := registry.NewCollector(ctx, name); !errors.Is(err, context.Canceled) {
			t.Fatalf("NewCollector(%s) error = %v, want context.Canceled", name, err)
		}
	}
}

func assertScalarSample(t *testing.T, registry *source.Registry, name string, want float64, when time.Time) {
	t.Helper()
	collector, err := registry.NewCollector(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scalar, ok := sample.(source.ScalarSample)
	if !ok || math.Abs(scalar.Value-want) > 1e-9 || scalar.Time != when {
		t.Fatalf("%s sample = %#v, want %v at %s", name, sample, want, when)
	}
}

func pointerValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
