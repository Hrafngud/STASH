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

const (
	RAMUsedName = "ram.used"
	RAMFreeName = "ram.free"
)

// MemorySnapshot represents Linux's total and readily available memory.
// ram.free uses MemAvailable, and ram.used is MemTotal minus MemAvailable.
type MemorySnapshot struct {
	TotalBytes     uint64
	AvailableBytes uint64
}

func ParseMemInfo(data []byte) (MemorySnapshot, error) {
	var snapshot MemorySnapshot
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || (fields[0] != "MemTotal:" && fields[0] != "MemAvailable:") {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		if seen[name] {
			return MemorySnapshot{}, fmt.Errorf("parse /proc/meminfo line %d: duplicate %s", lineNumber, name)
		}
		seen[name] = true
		if len(fields) != 3 || fields[2] != "kB" {
			return MemorySnapshot{}, fmt.Errorf("parse /proc/meminfo line %d: %s must be one kB value", lineNumber, name)
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || kilobytes > math.MaxUint64/1024 {
			return MemorySnapshot{}, fmt.Errorf("parse /proc/meminfo line %d: invalid %s value %q", lineNumber, name, fields[1])
		}
		bytes := kilobytes * 1024
		if name == "MemTotal" {
			snapshot.TotalBytes = bytes
		} else {
			snapshot.AvailableBytes = bytes
		}
	}
	if err := scanner.Err(); err != nil {
		return MemorySnapshot{}, fmt.Errorf("scan /proc/meminfo: %w", err)
	}
	if !seen["MemTotal"] || snapshot.TotalBytes == 0 {
		return MemorySnapshot{}, fmt.Errorf("parse /proc/meminfo: positive MemTotal is missing")
	}
	if !seen["MemAvailable"] {
		return MemorySnapshot{}, fmt.Errorf("parse /proc/meminfo: MemAvailable is missing")
	}
	if snapshot.AvailableBytes > snapshot.TotalBytes {
		return MemorySnapshot{}, fmt.Errorf("parse /proc/meminfo: MemAvailable exceeds MemTotal")
	}
	return snapshot, nil
}

type memoryMetric uint8

const (
	memoryUsed memoryMetric = iota
	memoryFree
)

type memoryCollector struct {
	read   ReadFile
	now    func() time.Time
	metric memoryMetric
}

func (collector *memoryCollector) Collect(ctx context.Context) (source.Sample, error) {
	snapshot, err := readAndParse(ctx, collector.read, "memory", ParseMemInfo)
	if err != nil {
		return nil, fmt.Errorf("collect memory: %w", err)
	}
	value := snapshot.AvailableBytes
	if collector.metric == memoryUsed {
		value = snapshot.TotalBytes - snapshot.AvailableBytes
	}
	return source.ScalarSample{Value: float64(value), Time: collector.now()}, nil
}
