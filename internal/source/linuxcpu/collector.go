package linuxcpu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const ProcStatPath = "/proc/stat"

// ReadStat supplies one /proc/stat observation. Tests inject fixture readers;
// production uses ReadProcStat.
type ReadStat func(context.Context) ([]byte, error)

// ReadProcStat reads the Linux kernel CPU statistics file.
func ReadProcStat(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(ProcStatPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ProcStatPath, err)
	}
	return data, nil
}

type selectorKind uint8

const (
	selectAggregate selectorKind = iota
	selectCore
	selectCores
)

type selector struct {
	kind selectorKind
	core int
}

type usageCollector struct {
	read     ReadStat
	now      func() time.Time
	selector selector
	previous Snapshot
}

func newUsageCollector(ctx context.Context, read ReadStat, now func() time.Time, selected selector) (*usageCollector, error) {
	if read == nil {
		return nil, fmt.Errorf("CPU stat reader is nil")
	}
	if now == nil {
		now = time.Now
	}
	baseline, err := readSnapshot(ctx, read)
	if err != nil {
		return nil, fmt.Errorf("read CPU utilization baseline: %w", err)
	}
	if selected.kind == selectCore {
		if _, ok := baseline.Core(selected.core); !ok {
			return nil, fmt.Errorf("CPU core %d is unavailable", selected.core)
		}
	}
	return &usageCollector{read: read, now: now, selector: selected, previous: baseline}, nil
}

func (collector *usageCollector) Collect(ctx context.Context) (source.Sample, error) {
	current, err := readSnapshot(ctx, collector.read)
	if err != nil {
		return nil, fmt.Errorf("collect CPU utilization: %w", err)
	}

	sample, err := collector.makeSample(current)
	if err != nil {
		if errors.Is(err, ErrCounterReset) {
			collector.previous = current
		}
		return nil, fmt.Errorf("collect CPU utilization: %w", err)
	}
	collector.previous = current
	return sample, nil
}

func (collector *usageCollector) makeSample(current Snapshot) (source.Sample, error) {
	switch collector.selector.kind {
	case selectAggregate:
		value, err := Utilization(collector.previous.Aggregate(), current.Aggregate())
		if err != nil {
			return nil, err
		}
		return source.ScalarSample{Value: value, Time: collector.now()}, nil
	case selectCore:
		previous, previousOK := collector.previous.Core(collector.selector.core)
		currentCounters, currentOK := current.Core(collector.selector.core)
		if !previousOK || !currentOK {
			return nil, fmt.Errorf("CPU core %d is unavailable", collector.selector.core)
		}
		value, err := Utilization(previous, currentCounters)
		if err != nil {
			return nil, err
		}
		return source.ScalarSample{Value: value, Time: collector.now()}, nil
	case selectCores:
		previousIndices := collector.previous.CoreIndices()
		currentIndices := current.CoreIndices()
		if !equalIndices(previousIndices, currentIndices) {
			return nil, fmt.Errorf("logical CPU set changed from %v to %v", previousIndices, currentIndices)
		}
		values := make([]float64, len(currentIndices))
		for position, coreIndex := range currentIndices {
			previous, _ := collector.previous.Core(coreIndex)
			currentCounters, _ := current.Core(coreIndex)
			value, err := Utilization(previous, currentCounters)
			if err != nil {
				return nil, fmt.Errorf("CPU core %d: %w", coreIndex, err)
			}
			values[position] = value
		}
		return source.VectorSample{Values: values, Time: collector.now()}, nil
	default:
		return nil, fmt.Errorf("invalid CPU utilization selector")
	}
}

func readSnapshot(ctx context.Context, read ReadStat) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, err := read(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := ParseStat(data)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func equalIndices(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
