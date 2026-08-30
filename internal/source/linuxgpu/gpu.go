// Package linuxgpu collects optional Linux GPU telemetry from local kernel
// interfaces. Backends are selected at runtime; unsupported devices remain
// explicitly unavailable and no external command is executed.
package linuxgpu

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const drmPath = "sys/class/drm"

type metricKind uint8

const (
	metricUsage metricKind = iota
	metricFrequency
	metricTemperature
	metricPower
	metricVRAM
)

type metricProbe struct {
	path              string
	kind              metricKind
	maximum           uint64
	read              func() (float64, error)
	unavailableReason string
}

type gpuProbe struct {
	card  string
	usage metricProbe
	freq  metricProbe
	temp  metricProbe
	power metricProbe
	vram  metricProbe
}

func detectGPU(ctx context.Context, filesystem fs.FS) (gpuProbe, error) {
	if err := checkInputs(ctx, filesystem); err != nil {
		return gpuProbe{}, err
	}
	entries, err := fs.ReadDir(filesystem, drmPath)
	if errors.Is(err, fs.ErrNotExist) {
		return gpuProbe{}, fmt.Errorf("no supported DRM GPU backend")
	}
	if err != nil {
		return gpuProbe{}, fmt.Errorf("read DRM directory: %w", err)
	}
	for _, entry := range entries {
		if !isCardName(entry.Name()) {
			continue
		}
		devicePath := drmPath + "/" + entry.Name() + "/device"
		vendor, err := fs.ReadFile(filesystem, devicePath+"/vendor")
		if err != nil || !strings.EqualFold(strings.TrimSpace(string(vendor)), "0x1002") {
			continue
		}
		return detectAMDGPU(filesystem, entry.Name(), devicePath), nil
	}
	return gpuProbe{}, fmt.Errorf("no supported DRM GPU backend")
}

func isCardName(name string) bool {
	if !strings.HasPrefix(name, "card") || len(name) == len("card") {
		return false
	}
	for _, character := range strings.TrimPrefix(name, "card") {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func detectAMDGPU(filesystem fs.FS, card, devicePath string) gpuProbe {
	probe := gpuProbe{card: card}
	probe.usage = probeMetric(filesystem, devicePath+"/gpu_busy_percent", metricUsage, 0, card)
	probe.vram = probeVRAM(filesystem, devicePath, card)

	hwmonPath, err := detectAMDGPUHWMon(filesystem, devicePath)
	if err != nil {
		reason := fmt.Sprintf("detect AMD GPU %s hwmon: %v", card, err)
		probe.freq = unavailableMetric(metricFrequency, reason)
		probe.temp = unavailableMetric(metricTemperature, reason)
		probe.power = unavailableMetric(metricPower, reason)
		return probe
	}
	if hwmonPath == "" {
		reason := fmt.Sprintf("AMD GPU %s has no readable amdgpu hwmon interface", card)
		probe.freq = unavailableMetric(metricFrequency, reason)
		probe.temp = unavailableMetric(metricTemperature, reason)
		probe.power = unavailableMetric(metricPower, reason)
		return probe
	}
	probe.freq = probeMetric(filesystem, hwmonPath+"/freq1_input", metricFrequency, 0, card)
	probe.temp = probeMetric(filesystem, hwmonPath+"/temp1_input", metricTemperature, 0, card)
	powerPath := hwmonPath + "/power1_average"
	if _, err := fs.Stat(filesystem, powerPath); errors.Is(err, fs.ErrNotExist) {
		powerPath = hwmonPath + "/power1_input"
	}
	probe.power = probeMetric(filesystem, powerPath, metricPower, 0, card)
	return probe
}

func detectAMDGPUHWMon(filesystem fs.FS, devicePath string) (string, error) {
	directory := devicePath + "/hwmon"
	entries, err := fs.ReadDir(filesystem, directory)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", directory, err)
	}
	for _, entry := range entries {
		path := directory + "/" + entry.Name()
		name, err := fs.ReadFile(filesystem, path+"/name")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read %s/name: %w", path, err)
		}
		if strings.TrimSpace(string(name)) == "amdgpu" {
			return path, nil
		}
	}
	return "", nil
}

func probeVRAM(filesystem fs.FS, devicePath, card string) metricProbe {
	totalPath := devicePath + "/mem_info_vram_total"
	total, err := readUnsignedFile(filesystem, totalPath, "byte")
	if err != nil {
		return unavailableMetric(metricVRAM, metricDetectionReason(card, totalPath, err))
	}
	if total == 0 {
		return unavailableMetric(metricVRAM, fmt.Sprintf("detect AMD GPU %s VRAM: total is zero", card))
	}
	return probeMetric(filesystem, devicePath+"/mem_info_vram_used", metricVRAM, total, card)
}

func probeMetric(filesystem fs.FS, path string, kind metricKind, maximum uint64, card string) metricProbe {
	probe := metricProbe{path: path, kind: kind, maximum: maximum}
	if _, err := readMetric(filesystem, probe); err != nil {
		return unavailableMetric(kind, metricDetectionReason(card, path, err))
	}
	return probe
}

func unavailableMetric(kind metricKind, reason string) metricProbe {
	return metricProbe{kind: kind, unavailableReason: reason}
}

func metricDetectionReason(card, path string, err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("AMD GPU %s does not expose %s", card, path[strings.LastIndexByte(path, '/')+1:])
	}
	return fmt.Sprintf("detect AMD GPU %s metric %s: %v", card, path, err)
}

func readMetric(filesystem fs.FS, metric metricProbe) (float64, error) {
	switch metric.kind {
	case metricUsage:
		value, err := readUnsignedFile(filesystem, metric.path, "percent")
		if err != nil {
			return 0, err
		}
		if value > 100 {
			return 0, fmt.Errorf("percent value %d is outside 0..100", value)
		}
		return float64(value), nil
	case metricFrequency:
		value, err := readUnsignedFile(filesystem, metric.path, "hertz")
		if err != nil {
			return 0, err
		}
		if value == 0 || value > 1_000_000_000_000 {
			return 0, fmt.Errorf("hertz value %d is outside the supported range", value)
		}
		return float64(value), nil
	case metricTemperature:
		data, err := fs.ReadFile(filesystem, metric.path)
		if err != nil {
			return 0, err
		}
		value, err := parseSigned(data, "millidegree Celsius")
		if err != nil {
			return 0, err
		}
		temperature := float64(value) / 1000
		if temperature < -273.15 || temperature > 1000 {
			return 0, fmt.Errorf("millidegree Celsius value %d is outside the supported range", value)
		}
		return temperature, nil
	case metricPower:
		value, err := readUnsignedFile(filesystem, metric.path, "microwatt")
		return float64(value) / 1_000_000, err
	case metricVRAM:
		value, err := readUnsignedFile(filesystem, metric.path, "byte")
		if err != nil {
			return 0, err
		}
		if value > metric.maximum {
			return 0, fmt.Errorf("VRAM usage %d exceeds total %d", value, metric.maximum)
		}
		return float64(value), nil
	default:
		return 0, fmt.Errorf("unknown GPU metric kind %d", metric.kind)
	}
}

func readUnsignedFile(filesystem fs.FS, path, unit string) (uint64, error) {
	data, err := fs.ReadFile(filesystem, path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 1 {
		return 0, fmt.Errorf("expected one %s value, got %d fields", unit, len(fields))
	}
	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", unit, fields[0], err)
	}
	return value, nil
}

func parseSigned(data []byte, unit string) (int64, error) {
	fields := strings.Fields(string(data))
	if len(fields) != 1 {
		return 0, fmt.Errorf("expected one %s value, got %d fields", unit, len(fields))
	}
	value, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", unit, fields[0], err)
	}
	return value, nil
}

type gpuCollector struct {
	filesystem fs.FS
	metric     metricProbe
	name       string
	now        func() time.Time
}

func (collector *gpuCollector) Collect(ctx context.Context) (source.Sample, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var value float64
	var err error
	if collector.metric.read != nil {
		value, err = collector.metric.read()
	} else {
		if collector.filesystem == nil {
			return nil, fmt.Errorf("collect %s: GPU filesystem is nil", collector.name)
		}
		value, err = readMetric(collector.filesystem, collector.metric)
	}
	if err != nil {
		return nil, fmt.Errorf("collect %s: %w", collector.name, err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("collect %s: non-finite value", collector.name)
	}
	return source.ScalarSample{Value: value, Time: collector.now()}, nil
}

func checkInputs(ctx context.Context, filesystem fs.FS) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if filesystem == nil {
		return fmt.Errorf("GPU filesystem is nil")
	}
	return nil
}
