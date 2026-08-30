package linuxproc

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const diskSectorBytes uint64 = 512

type DiskCounters struct {
	ReadsCompleted  uint64
	ReadSectors     uint64
	WritesCompleted uint64
	WrittenSectors  uint64
}

func ParseDiskStats(data []byte) (map[string]DiskCounters, error) {
	devices := make(map[string]DiskCounters)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 14 {
			return nil, fmt.Errorf("parse /proc/diskstats line %d: expected at least 14 fields, got %d", lineNumber, len(fields))
		}
		name := fields[2]
		if name == "" || strings.ContainsAny(name, " \t\r\n") {
			return nil, fmt.Errorf("parse /proc/diskstats line %d: invalid device name %q", lineNumber, name)
		}
		if _, duplicate := devices[name]; duplicate {
			return nil, fmt.Errorf("parse /proc/diskstats line %d: duplicate device %q", lineNumber, name)
		}
		indices := [...]int{3, 5, 7, 9}
		values := [4]uint64{}
		for index, fieldIndex := range indices {
			value, err := strconv.ParseUint(fields[fieldIndex], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse /proc/diskstats line %d (%s): invalid field %d %q", lineNumber, name, fieldIndex+1, fields[fieldIndex])
			}
			values[index] = value
		}
		devices[name] = DiskCounters{
			ReadsCompleted: values[0], ReadSectors: values[1],
			WritesCompleted: values[2], WrittenSectors: values[3],
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/diskstats: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("parse /proc/diskstats: no block devices found")
	}
	return devices, nil
}

type diskMetric uint8

const (
	diskRead diskMetric = iota
	diskWrite
	diskOperations
)

func (metric diskMetric) counter(counters DiskCounters) (uint64, uint64) {
	switch metric {
	case diskRead:
		return counters.ReadSectors, diskSectorBytes
	case diskWrite:
		return counters.WrittenSectors, diskSectorBytes
	default:
		return counters.ReadsCompleted + counters.WritesCompleted, 1
	}
}

func diskRate(previous, current DiskCounters, metric diskMetric, previousAt, currentAt time.Time) (float64, error) {
	if metric == diskOperations {
		if current.ReadsCompleted < previous.ReadsCompleted {
			return 0, fmt.Errorf("%w: completed reads moved from %d to %d", ErrCounterReset, previous.ReadsCompleted, current.ReadsCompleted)
		}
		if current.WritesCompleted < previous.WritesCompleted {
			return 0, fmt.Errorf("%w: completed writes moved from %d to %d", ErrCounterReset, previous.WritesCompleted, current.WritesCompleted)
		}
		readDelta := current.ReadsCompleted - previous.ReadsCompleted
		writeDelta := current.WritesCompleted - previous.WritesCompleted
		if readDelta > math.MaxUint64-writeDelta {
			return 0, fmt.Errorf("rate delta overflow")
		}
		return counterRate(0, readDelta+writeDelta, previousAt, currentAt, 1)
	}
	previousValue, multiplier := metric.counter(previous)
	currentValue, _ := metric.counter(current)
	return counterRate(previousValue, currentValue, previousAt, currentAt, multiplier)
}

type diskCollector struct {
	read       ReadFile
	now        func() time.Time
	device     string
	metric     diskMetric
	previous   DiskCounters
	previousAt time.Time
}

func newDiskCollector(ctx context.Context, read ReadFile, now func() time.Time, device string, metric diskMetric) (*diskCollector, error) {
	snapshot, err := readAndParse(ctx, read, "disk", ParseDiskStats)
	if err != nil {
		return nil, fmt.Errorf("read disk baseline: %w", err)
	}
	counters, ok := snapshot[device]
	if !ok {
		return nil, fmt.Errorf("block device %s is unavailable", device)
	}
	return &diskCollector{read: read, now: now, device: device, metric: metric, previous: counters, previousAt: now()}, nil
}

func (collector *diskCollector) Collect(ctx context.Context) (source.Sample, error) {
	snapshot, err := readAndParse(ctx, collector.read, "disk", ParseDiskStats)
	if err != nil {
		return nil, fmt.Errorf("collect block device %s: %w", collector.device, err)
	}
	current, ok := snapshot[collector.device]
	if !ok {
		return nil, fmt.Errorf("collect block device %s: device disappeared", collector.device)
	}
	when := collector.now()
	value, err := diskRate(collector.previous, current, collector.metric, collector.previousAt, when)
	if err != nil {
		if isCounterReset(err) {
			collector.previous, collector.previousAt = current, when
		}
		return nil, fmt.Errorf("collect block device %s: %w", collector.device, err)
	}
	collector.previous, collector.previousAt = current, when
	return source.ScalarSample{Value: value, Time: when}, nil
}
