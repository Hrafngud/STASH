package linuxcpu

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zalmo/stash/internal/source"
)

const (
	AggregateUsageName = "cpu.usage"
	CoresUsageName     = "cpu.cores.usage"
)

// RegisterDefaultUsage detects and registers Linux CPU usage sources backed by
// /proc/stat.
func RegisterDefaultUsage(ctx context.Context, registry *source.Registry) error {
	return RegisterUsage(ctx, registry, ReadProcStat, time.Now)
}

// RegisterUsage probes a CPU statistics backend, records explicit
// availability, and registers aggregate, per-core, and ordered-vector usage
// sources. The clock is injected for deterministic sample timestamps.
func RegisterUsage(ctx context.Context, registry *source.Registry, read ReadStat, now func() time.Time) error {
	if registry == nil {
		return fmt.Errorf("register CPU usage sources: registry is nil")
	}
	if read == nil {
		return fmt.Errorf("register CPU usage sources: CPU stat reader is nil")
	}
	if now == nil {
		now = time.Now
	}

	probe, probeErr := readSnapshot(ctx, read)
	if probeErr != nil {
		if errors.Is(probeErr, context.Canceled) || errors.Is(probeErr, context.DeadlineExceeded) {
			return fmt.Errorf("register CPU usage sources: %w", probeErr)
		}
		reason := probeErr.Error()
		if err := registry.RegisterUnavailable(usageInfo(AggregateUsageName, source.KindScalar), reason); err != nil {
			return err
		}
		if err := registry.RegisterUnavailable(usageInfo(CoresUsageName, source.KindVector), reason); err != nil {
			return err
		}
		return nil
	}

	register := func(name string, kind source.Kind, selected selector) error {
		factory := func(factoryContext context.Context) (source.Collector, error) {
			return newUsageCollector(factoryContext, read, now, selected)
		}
		return registry.RegisterAvailable(usageInfo(name, kind), factory)
	}
	if err := register(AggregateUsageName, source.KindScalar, selector{kind: selectAggregate}); err != nil {
		return err
	}
	for _, coreIndex := range probe.CoreIndices() {
		name := fmt.Sprintf("cpu.core.%d.usage", coreIndex)
		if err := register(name, source.KindScalar, selector{kind: selectCore, core: coreIndex}); err != nil {
			return err
		}
	}
	if err := register(CoresUsageName, source.KindVector, selector{kind: selectCores}); err != nil {
		return err
	}
	return nil
}

func usageInfo(name string, kind source.Kind) source.Info {
	minimum, maximum := 0.0, 100.0
	return source.Info{
		Name:       name,
		Kind:       kind,
		Unit:       "%",
		NaturalMin: &minimum,
		NaturalMax: &maximum,
	}
}
