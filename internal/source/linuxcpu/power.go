package linuxcpu

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const sysPowercapPath = "sys/class/powercap"

type powerZone struct {
	energyPath     string
	maxMicrojoules uint64
}

type powerProbe struct {
	zones []powerZone
}

func detectPower(ctx context.Context, filesystem fs.FS) (powerProbe, error) {
	if err := checkHardwareInputs(ctx, filesystem); err != nil {
		return powerProbe{}, err
	}
	entries, err := fs.ReadDir(filesystem, sysPowercapPath)
	if errors.Is(err, fs.ErrNotExist) {
		return powerProbe{}, fmt.Errorf(unavailablePowerReason)
	}
	if err != nil {
		return powerProbe{}, fmt.Errorf("read powercap directory: %w", err)
	}

	probe := powerProbe{}
	for _, entry := range entries {
		directory := sysPowercapPath + "/" + entry.Name()
		nameData, err := fs.ReadFile(filesystem, directory+"/name")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return powerProbe{}, fmt.Errorf("read powercap zone name %s: %w", directory, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(nameData)), "package-") {
			continue
		}

		energyPath := directory + "/energy_uj"
		energy, err := readMicrojoules(filesystem, energyPath)
		if err != nil {
			return powerProbe{}, fmt.Errorf("detect CPU package energy: %w", err)
		}
		maximum, err := readMicrojoules(filesystem, directory+"/max_energy_range_uj")
		if err != nil {
			return powerProbe{}, fmt.Errorf("detect CPU package energy range: %w", err)
		}
		if maximum == 0 || energy > maximum {
			return powerProbe{}, fmt.Errorf("detect CPU package energy: value %d is outside range 0..%d", energy, maximum)
		}
		probe.zones = append(probe.zones, powerZone{energyPath: energyPath, maxMicrojoules: maximum})
	}
	if len(probe.zones) == 0 {
		return powerProbe{}, fmt.Errorf(unavailablePowerReason)
	}
	return probe, nil
}

func readMicrojoules(filesystem fs.FS, path string) (uint64, error) {
	data, err := fs.ReadFile(filesystem, path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 1 {
		return 0, fmt.Errorf("parse %s: expected one microjoule value, got %d fields", path, len(fields))
	}
	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: invalid microjoule value %q: %w", path, fields[0], err)
	}
	return value, nil
}

type powerCollector struct {
	filesystem fs.FS
	zones      []powerZone
	previous   []uint64
	previousAt time.Time
	now        func() time.Time
}

func newPowerCollector(ctx context.Context, filesystem fs.FS, zones []powerZone, now func() time.Time) (*powerCollector, error) {
	if err := checkHardwareInputs(ctx, filesystem); err != nil {
		return nil, err
	}
	previous := make([]uint64, len(zones))
	for index, zone := range zones {
		value, err := readMicrojoules(filesystem, zone.energyPath)
		if err != nil {
			return nil, fmt.Errorf("read CPU power baseline: %w", err)
		}
		if value > zone.maxMicrojoules {
			return nil, fmt.Errorf("read CPU power baseline: energy %d exceeds range %d", value, zone.maxMicrojoules)
		}
		previous[index] = value
	}
	return &powerCollector{
		filesystem: filesystem,
		zones:      append([]powerZone(nil), zones...),
		previous:   previous,
		previousAt: now(),
		now:        now,
	}, nil
}

func (collector *powerCollector) Collect(ctx context.Context) (source.Sample, error) {
	if err := checkHardwareInputs(ctx, collector.filesystem); err != nil {
		return nil, err
	}
	current := make([]uint64, len(collector.zones))
	for index, zone := range collector.zones {
		value, err := readMicrojoules(collector.filesystem, zone.energyPath)
		if err != nil {
			return nil, fmt.Errorf("collect CPU power: %w", err)
		}
		if value > zone.maxMicrojoules {
			return nil, fmt.Errorf("collect CPU power: energy %d exceeds range %d", value, zone.maxMicrojoules)
		}
		current[index] = value
	}
	when := collector.now()
	elapsed := when.Sub(collector.previousAt)
	if elapsed <= 0 {
		return nil, fmt.Errorf("collect CPU power: sample interval must be positive")
	}

	var deltaMicrojoules uint64
	for index, zone := range collector.zones {
		delta := energyDelta(collector.previous[index], current[index], zone.maxMicrojoules)
		if ^uint64(0)-deltaMicrojoules < delta {
			return nil, fmt.Errorf("collect CPU power: energy delta overflow")
		}
		deltaMicrojoules += delta
	}
	collector.previous, collector.previousAt = current, when
	value := float64(deltaMicrojoules) / 1_000_000 / elapsed.Seconds()
	return source.ScalarSample{Value: value, Time: when}, nil
}

func energyDelta(previous, current, maximum uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	return maximum - previous + current
}
