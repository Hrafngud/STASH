package linuxcpu

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const (
	sysCPUPath     = "sys/devices/system/cpu"
	sysHWMonPath   = "sys/class/hwmon"
	sysThermalPath = "sys/class/thermal"
)

type frequencyCore struct {
	index     int
	path      string
	minimumHz *float64
	maximumHz *float64
}

type frequencyProbe struct {
	cores []frequencyCore
}

func detectFrequency(ctx context.Context, filesystem fs.FS) (frequencyProbe, error) {
	if err := checkHardwareInputs(ctx, filesystem); err != nil {
		return frequencyProbe{}, err
	}
	entries, err := fs.ReadDir(filesystem, sysCPUPath)
	if err != nil {
		return frequencyProbe{}, fmt.Errorf("read CPU sysfs directory: %w", err)
	}

	probe := frequencyProbe{}
	for _, entry := range entries {
		coreIndex, ok := cpuDirectoryIndex(entry.Name())
		if !ok {
			continue
		}
		core, found, err := detectCoreFrequency(filesystem, coreIndex)
		if err != nil {
			return frequencyProbe{}, err
		}
		if found {
			probe.cores = append(probe.cores, core)
		}
	}
	if len(probe.cores) == 0 {
		return frequencyProbe{}, fmt.Errorf("no readable per-CPU cpufreq source")
	}
	sort.Slice(probe.cores, func(left, right int) bool {
		return probe.cores[left].index < probe.cores[right].index
	})
	return probe, nil
}

func detectCoreFrequency(filesystem fs.FS, coreIndex int) (frequencyCore, bool, error) {
	directory := fmt.Sprintf("%s/cpu%d/cpufreq", sysCPUPath, coreIndex)
	currentCandidates := []string{
		directory + "/cpuinfo_cur_freq",
		directory + "/scaling_cur_freq",
	}
	var currentPath string
	var currentErrors []error
	for _, candidate := range currentCandidates {
		_, exists, err := readOptionalKHz(filesystem, candidate)
		if err != nil {
			currentErrors = append(currentErrors, err)
			continue
		}
		if exists {
			currentPath = candidate
			break
		}
	}
	if currentPath == "" {
		if len(currentErrors) != 0 {
			return frequencyCore{}, false, fmt.Errorf("detect CPU core %d frequency: %w", coreIndex, errors.Join(currentErrors...))
		}
		return frequencyCore{}, false, nil
	}

	core := frequencyCore{index: coreIndex, path: currentPath}
	rangeCandidates := [][2]string{
		{directory + "/cpuinfo_min_freq", directory + "/cpuinfo_max_freq"},
		{directory + "/scaling_min_freq", directory + "/scaling_max_freq"},
	}
	for _, candidate := range rangeCandidates {
		minimum, hasMinimum, err := readOptionalKHz(filesystem, candidate[0])
		if err != nil {
			continue
		}
		maximum, hasMaximum, err := readOptionalKHz(filesystem, candidate[1])
		if err != nil {
			continue
		}
		if !hasMinimum || !hasMaximum {
			continue
		}
		if minimum >= maximum {
			return frequencyCore{}, false, fmt.Errorf("detect CPU core %d frequency range: minimum %.0f Hz must be less than maximum %.0f Hz", coreIndex, minimum, maximum)
		}
		core.minimumHz, core.maximumHz = floatPointer(minimum), floatPointer(maximum)
		break
	}
	return core, true, nil
}

func cpuDirectoryIndex(name string) (int, bool) {
	if !strings.HasPrefix(name, "cpu") {
		return 0, false
	}
	suffix := strings.TrimPrefix(name, "cpu")
	if suffix == "" || !allDecimalDigits(suffix) {
		return 0, false
	}
	index, err := strconv.ParseInt(suffix, 10, 32)
	return int(index), err == nil
}

func readOptionalKHz(filesystem fs.FS, path string) (float64, bool, error) {
	data, err := fs.ReadFile(filesystem, path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %w", path, err)
	}
	value, err := parseKHz(data)
	if err != nil {
		return 0, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return value, true, nil
}

func parseKHz(data []byte) (float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) != 1 {
		return 0, fmt.Errorf("expected one kHz value, got %d fields", len(fields))
	}
	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid kHz value %q: %w", fields[0], err)
	}
	if value == 0 || value > math.MaxUint64/1000 {
		return 0, fmt.Errorf("kHz value %q is outside the supported range", fields[0])
	}
	return float64(value) * 1000, nil
}

type frequencyCollector struct {
	filesystem fs.FS
	cores      []frequencyCore
	selected   selector
	now        func() time.Time
}

func (collector *frequencyCollector) Collect(ctx context.Context) (source.Sample, error) {
	if err := checkHardwareInputs(ctx, collector.filesystem); err != nil {
		return nil, err
	}
	values := make([]float64, 0, len(collector.cores))
	for _, core := range collector.cores {
		if collector.selected.kind == selectCore && core.index != collector.selected.core {
			continue
		}
		value, exists, err := readOptionalKHz(collector.filesystem, core.path)
		if err != nil {
			return nil, fmt.Errorf("collect CPU core %d frequency: %w", core.index, err)
		}
		if !exists {
			return nil, fmt.Errorf("collect CPU core %d frequency: source disappeared", core.index)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("collect CPU frequency: selected core is unavailable")
	}
	when := collector.now()
	switch collector.selected.kind {
	case selectAggregate:
		return source.ScalarSample{Value: average(values), Time: when}, nil
	case selectCore:
		return source.ScalarSample{Value: values[0], Time: when}, nil
	case selectCores:
		return source.VectorSample{Values: values, Time: when}, nil
	default:
		return nil, fmt.Errorf("collect CPU frequency: invalid selector")
	}
}

type temperatureProbe struct {
	paths []string
}

func detectTemperature(ctx context.Context, filesystem fs.FS) (temperatureProbe, error) {
	if err := checkHardwareInputs(ctx, filesystem); err != nil {
		return temperatureProbe{}, err
	}
	paths, err := detectHWMonTemperature(filesystem)
	if err != nil {
		return temperatureProbe{}, err
	}
	if len(paths) == 0 {
		paths, err = detectThermalTemperature(filesystem)
		if err != nil {
			return temperatureProbe{}, err
		}
	}
	if len(paths) == 0 {
		return temperatureProbe{}, fmt.Errorf("no reliable CPU package temperature sensor")
	}
	sort.Strings(paths)
	return temperatureProbe{paths: paths}, nil
}

func detectHWMonTemperature(filesystem fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(filesystem, sysHWMonPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hwmon directory: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "hwmon") {
			continue
		}
		directory := sysHWMonPath + "/" + entry.Name()
		nameData, err := fs.ReadFile(filesystem, directory+"/name")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s/name: %w", directory, err)
		}
		driver := strings.TrimSpace(string(nameData))
		if driver != "coretemp" && driver != "k10temp" && driver != "zenpower" {
			continue
		}
		sensors, err := labeledTemperatureSensors(filesystem, directory)
		if err != nil {
			return nil, err
		}
		switch driver {
		case "coretemp":
			for label, path := range sensors {
				if strings.HasPrefix(label, "Package id ") {
					if _, exists, err := readOptionalMilliCelsius(filesystem, path); err != nil {
						return nil, fmt.Errorf("detect CPU temperature: %w", err)
					} else if exists {
						paths = append(paths, path)
					}
				}
			}
		case "k10temp", "zenpower":
			for _, label := range []string{"Tdie", "Tctl"} {
				if path, ok := sensors[label]; ok {
					if _, exists, err := readOptionalMilliCelsius(filesystem, path); err != nil {
						return nil, fmt.Errorf("detect CPU temperature: %w", err)
					} else if exists {
						paths = append(paths, path)
						break
					}
				}
			}
		}
	}
	return paths, nil
}

func labeledTemperatureSensors(filesystem fs.FS, directory string) (map[string]string, error) {
	entries, err := fs.ReadDir(filesystem, directory)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", directory, err)
	}
	sensors := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "temp") || !strings.HasSuffix(name, "_label") {
			continue
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(name, "temp"), "_label")
		if middle == "" || !allDecimalDigits(middle) {
			continue
		}
		labelData, err := fs.ReadFile(filesystem, directory+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", directory, name, err)
		}
		inputPath := directory + "/temp" + middle + "_input"
		sensors[strings.TrimSpace(string(labelData))] = inputPath
	}
	return sensors, nil
}

func detectThermalTemperature(filesystem fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(filesystem, sysThermalPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read thermal directory: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}
		directory := sysThermalPath + "/" + entry.Name()
		typeData, err := fs.ReadFile(filesystem, directory+"/type")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s/type: %w", directory, err)
		}
		zoneType := strings.ToLower(strings.TrimSpace(string(typeData)))
		if zoneType != "x86_pkg_temp" && zoneType != "cpu-thermal" && zoneType != "cpu_thermal" {
			continue
		}
		path := directory + "/temp"
		if _, exists, err := readOptionalMilliCelsius(filesystem, path); err != nil {
			return nil, fmt.Errorf("detect CPU temperature: %w", err)
		} else if exists {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func readOptionalMilliCelsius(filesystem fs.FS, path string) (float64, bool, error) {
	data, err := fs.ReadFile(filesystem, path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %w", path, err)
	}
	value, err := parseMilliCelsius(data)
	if err != nil {
		return 0, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return value, true, nil
}

func parseMilliCelsius(data []byte) (float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) != 1 {
		return 0, fmt.Errorf("expected one millidegree Celsius value, got %d fields", len(fields))
	}
	value, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid millidegree Celsius value %q: %w", fields[0], err)
	}
	temperature := float64(value) / 1000
	if temperature < -273.15 || temperature > 1000 {
		return 0, fmt.Errorf("millidegree Celsius value %q is outside the supported range", fields[0])
	}
	return temperature, nil
}

type temperatureCollector struct {
	filesystem fs.FS
	paths      []string
	now        func() time.Time
}

func (collector *temperatureCollector) Collect(ctx context.Context) (source.Sample, error) {
	if err := checkHardwareInputs(ctx, collector.filesystem); err != nil {
		return nil, err
	}
	values := make([]float64, len(collector.paths))
	for index, path := range collector.paths {
		value, exists, err := readOptionalMilliCelsius(collector.filesystem, path)
		if err != nil {
			return nil, fmt.Errorf("collect CPU temperature: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("collect CPU temperature: sensor %s disappeared", path)
		}
		values[index] = value
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("collect CPU temperature: no sensors configured")
	}
	return source.ScalarSample{Value: average(values), Time: collector.now()}, nil
}

func average(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func checkHardwareInputs(ctx context.Context, filesystem fs.FS) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if filesystem == nil {
		return fmt.Errorf("hardware filesystem is nil")
	}
	return nil
}

func floatPointer(value float64) *float64 {
	return &value
}
