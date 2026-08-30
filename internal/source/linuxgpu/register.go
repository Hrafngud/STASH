package linuxgpu

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const (
	UsageName       = "gpu.usage"
	FrequencyName   = "gpu.freq"
	TemperatureName = "gpu.temp"
	PowerName       = "gpu.power"
	VRAMName        = "gpu.vram"
)

type sourceDefinition struct {
	name   string
	unit   string
	kind   metricKind
	metric func(gpuProbe) metricProbe
}

var sourceDefinitions = []sourceDefinition{
	{UsageName, "%", metricUsage, func(probe gpuProbe) metricProbe { return probe.usage }},
	{FrequencyName, "Hz", metricFrequency, func(probe gpuProbe) metricProbe { return probe.freq }},
	{TemperatureName, "°C", metricTemperature, func(probe gpuProbe) metricProbe { return probe.temp }},
	{PowerName, "W", metricPower, func(probe gpuProbe) metricProbe { return probe.power }},
	{VRAMName, "B", metricVRAM, func(probe gpuProbe) metricProbe { return probe.vram }},
}

// RegisterDefault detects optional GPU telemetry from local Linux sysfs APIs
// and, when no AMDGPU device is selected, a runtime-loaded NVIDIA NVML API.
func RegisterDefault(ctx context.Context, registry *source.Registry) error {
	return registerWithNVML(ctx, registry, os.DirFS("/"), time.Now, loadDynamicNVML)
}

// Register performs runtime feature detection with injected filesystem and
// clock dependencies so tests never require GPU hardware or vendor libraries.
func Register(ctx context.Context, registry *source.Registry, filesystem fs.FS, now func() time.Time) error {
	return registerWithNVML(ctx, registry, filesystem, now, nil)
}

func registerWithNVML(ctx context.Context, registry *source.Registry, filesystem fs.FS, now func() time.Time, load nvmlLoader) error {
	if registry == nil {
		return fmt.Errorf("register Linux GPU sources: registry is nil")
	}
	if err := checkInputs(ctx, filesystem); err != nil {
		return fmt.Errorf("register Linux GPU sources: %w", err)
	}
	if now == nil {
		now = time.Now
	}

	probe, detectionErr := detectGPU(ctx, filesystem)
	if errors.Is(detectionErr, context.Canceled) || errors.Is(detectionErr, context.DeadlineExceeded) {
		return fmt.Errorf("register Linux GPU sources: %w", detectionErr)
	}
	if detectionErr != nil && load != nil {
		amdErr := detectionErr
		probe, detectionErr = detectNVIDIA(ctx, load)
		if errors.Is(detectionErr, context.Canceled) || errors.Is(detectionErr, context.DeadlineExceeded) {
			return fmt.Errorf("register Linux GPU sources: %w", detectionErr)
		}
		if detectionErr != nil {
			detectionErr = fmt.Errorf("%v; NVIDIA backend unavailable: %w", amdErr, detectionErr)
		}
	}
	for _, definition := range sourceDefinitions {
		metric := definition.metric(probe)
		info := metricInfo(definition.name, definition.unit, definition.kind, metric)
		if detectionErr != nil {
			if err := registry.RegisterUnavailable(info, detectionErr.Error()); err != nil {
				return err
			}
			continue
		}
		if metric.unavailableReason != "" {
			if err := registry.RegisterUnavailable(info, metric.unavailableReason); err != nil {
				return err
			}
			continue
		}
		name := definition.name
		factory := func(factoryContext context.Context) (source.Collector, error) {
			if err := checkInputs(factoryContext, filesystem); err != nil {
				return nil, err
			}
			return &gpuCollector{filesystem: filesystem, metric: metric, name: name, now: now}, nil
		}
		if err := registry.RegisterAvailable(info, factory); err != nil {
			return err
		}
	}
	return nil
}

func metricInfo(name, unit string, kind metricKind, metric metricProbe) source.Info {
	info := source.Info{Name: name, Kind: source.KindScalar, Unit: unit}
	switch kind {
	case metricUsage:
		minimum, maximum := 0.0, 100.0
		info.NaturalMin, info.NaturalMax = &minimum, &maximum
	case metricVRAM:
		if metric.maximum > 0 {
			minimum, maximum := 0.0, float64(metric.maximum)
			info.NaturalMin, info.NaturalMax = &minimum, &maximum
		}
	}
	return info
}
