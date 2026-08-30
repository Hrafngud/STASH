package linuxgpu

import (
	"context"
	"fmt"
	"math"
)

// nvmlClient is the narrow, injectable surface needed from NVIDIA's stable
// Management Library. The production implementation is loaded at runtime;
// tests provide fixtures without a GPU or proprietary driver.
type nvmlClient interface {
	DeviceCount() (uint32, error)
	Device(uint32) (nvmlDevice, error)
}

type nvmlDevice interface {
	UsagePercent() (uint32, error)
	GraphicsClockMHz() (uint32, error)
	TemperatureC() (uint32, error)
	PowerMilliwatts() (uint32, error)
	MemoryBytes() (used uint64, total uint64, err error)
}

type nvmlLoader func() (nvmlClient, error)

func detectNVIDIA(ctx context.Context, load nvmlLoader) (gpuProbe, error) {
	if ctx == nil {
		return gpuProbe{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return gpuProbe{}, err
	}
	if load == nil {
		return gpuProbe{}, fmt.Errorf("NVML loader is unavailable")
	}
	client, err := load()
	if err != nil {
		return gpuProbe{}, fmt.Errorf("load NVIDIA NVML: %w", err)
	}
	if client == nil {
		return gpuProbe{}, fmt.Errorf("load NVIDIA NVML: loader returned nil client")
	}
	count, err := client.DeviceCount()
	if err != nil {
		return gpuProbe{}, fmt.Errorf("query NVIDIA device count: %w", err)
	}
	if count == 0 {
		return gpuProbe{}, fmt.Errorf("NVIDIA NVML reported no devices")
	}
	// Canonical unindexed gpu.* names select NVML device index zero. AMDGPU
	// sysfs is preferred before this detector, preserving the existing backend
	// on mixed-vendor hosts.
	device, err := client.Device(0)
	if err != nil {
		return gpuProbe{}, fmt.Errorf("open NVIDIA device 0: %w", err)
	}
	if device == nil {
		return gpuProbe{}, fmt.Errorf("open NVIDIA device 0: NVML returned nil device")
	}

	probe := gpuProbe{card: "NVML device 0"}
	probe.usage = probeNVMLMetric(metricUsage, "utilization", func() (float64, error) {
		value, err := device.UsagePercent()
		if err != nil {
			return 0, err
		}
		if value > 100 {
			return 0, fmt.Errorf("percent value %d is outside 0..100", value)
		}
		return float64(value), nil
	})
	probe.freq = probeNVMLMetric(metricFrequency, "graphics clock", func() (float64, error) {
		value, err := device.GraphicsClockMHz()
		if err != nil {
			return 0, err
		}
		if value == 0 || uint64(value) > 1_000_000 {
			return 0, fmt.Errorf("megahertz value %d is outside the supported range", value)
		}
		return float64(value) * 1_000_000, nil
	})
	probe.temp = probeNVMLMetric(metricTemperature, "temperature", func() (float64, error) {
		value, err := device.TemperatureC()
		if err != nil {
			return 0, err
		}
		if value > 1000 {
			return 0, fmt.Errorf("Celsius value %d is outside the supported range", value)
		}
		return float64(value), nil
	})
	probe.power = probeNVMLMetric(metricPower, "power draw", func() (float64, error) {
		value, err := device.PowerMilliwatts()
		if err != nil {
			return 0, err
		}
		return float64(value) / 1000, nil
	})

	used, total, memoryErr := device.MemoryBytes()
	if memoryErr != nil {
		probe.vram = unavailableMetric(metricVRAM, fmt.Sprintf("NVIDIA NVML device 0 memory unavailable: %v", memoryErr))
	} else if total == 0 {
		probe.vram = unavailableMetric(metricVRAM, "NVIDIA NVML device 0 memory total is zero")
	} else if used > total {
		probe.vram = unavailableMetric(metricVRAM, fmt.Sprintf("NVIDIA NVML device 0 used memory %d exceeds total %d", used, total))
	} else {
		probe.vram = probeNVMLMetric(metricVRAM, "used VRAM", func() (float64, error) {
			currentUsed, currentTotal, err := device.MemoryBytes()
			if err != nil {
				return 0, err
			}
			if currentTotal == 0 || currentUsed > currentTotal {
				return 0, fmt.Errorf("used memory %d is invalid for total %d", currentUsed, currentTotal)
			}
			return float64(currentUsed), nil
		})
		probe.vram.maximum = total
	}
	return probe, nil
}

func probeNVMLMetric(kind metricKind, label string, read func() (float64, error)) metricProbe {
	probe := metricProbe{kind: kind, read: read}
	value, err := read()
	if err != nil {
		return unavailableMetric(kind, fmt.Sprintf("NVIDIA NVML device 0 %s unavailable: %v", label, err))
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return unavailableMetric(kind, fmt.Sprintf("NVIDIA NVML device 0 %s returned a non-finite value", label))
	}
	return probe
}
