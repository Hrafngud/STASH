package linuxproc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/zalmo/stash/internal/source"
)

func RegisterDefault(ctx context.Context, registry *source.Registry) error {
	return Register(ctx, registry, ReadMemInfo, ReadNetDev, ReadDiskStats, time.Now)
}

// Register detects and registers all additional procfs-backed Linux sources.
// Dynamic network and disk names are registered only after a successful probe.
func Register(ctx context.Context, registry *source.Registry, readMemory, readNetwork, readDisk ReadFile, now func() time.Time) error {
	if registry == nil {
		return fmt.Errorf("register Linux procfs sources: registry is nil")
	}
	if ctx == nil {
		return fmt.Errorf("register Linux procfs sources: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("register Linux procfs sources: %w", err)
	}
	if readMemory == nil || readNetwork == nil || readDisk == nil {
		return fmt.Errorf("register Linux procfs sources: reader is nil")
	}
	if now == nil {
		now = time.Now
	}

	memory, memoryErr := readAndParse(ctx, readMemory, "memory", ParseMemInfo)
	if isContextFailure(memoryErr) {
		return fmt.Errorf("register Linux procfs sources: %w", memoryErr)
	}
	if err := registerMemory(registry, readMemory, now, memory, memoryErr); err != nil {
		return err
	}

	network, networkErr := readAndParse(ctx, readNetwork, "network", ParseNetDev)
	if isContextFailure(networkErr) {
		return fmt.Errorf("register Linux procfs sources: %w", networkErr)
	}
	if networkErr == nil {
		if err := registerNetwork(registry, readNetwork, now, network); err != nil {
			return err
		}
	}

	disks, diskErr := readAndParse(ctx, readDisk, "disk", ParseDiskStats)
	if isContextFailure(diskErr) {
		return fmt.Errorf("register Linux procfs sources: %w", diskErr)
	}
	if diskErr == nil {
		if err := registerDisks(registry, readDisk, now, disks); err != nil {
			return err
		}
	}
	return nil
}

func registerMemory(registry *source.Registry, read ReadFile, now func() time.Time, probe MemorySnapshot, probeErr error) error {
	infos := []struct {
		name   string
		metric memoryMetric
	}{{RAMUsedName, memoryUsed}, {RAMFreeName, memoryFree}}
	for _, item := range infos {
		info := source.Info{Name: item.name, Kind: source.KindScalar, Unit: "B"}
		if probeErr != nil {
			if err := registry.RegisterUnavailable(info, probeErr.Error()); err != nil {
				return err
			}
			continue
		}
		minimum, maximum := 0.0, float64(probe.TotalBytes)
		info.NaturalMin, info.NaturalMax = &minimum, &maximum
		metric := item.metric
		if err := registry.RegisterAvailable(info, func(factoryContext context.Context) (source.Collector, error) {
			if err := factoryContext.Err(); err != nil {
				return nil, err
			}
			return &memoryCollector{read: read, now: now, metric: metric}, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func registerNetwork(registry *source.Registry, read ReadFile, now func() time.Time, probe map[string]NetworkCounters) error {
	names := sortedKeys(probe)
	metrics := []struct {
		suffix string
		unit   string
		metric networkMetric
	}{{"rx", "B/s", networkReceiveBytes}, {"tx", "B/s", networkTransmitBytes}, {"rx.packets", "packets/s", networkReceivePackets}, {"tx.packets", "packets/s", networkTransmitPackets}}
	for _, name := range names {
		for _, item := range metrics {
			interfaceName, metric := name, item.metric
			info := source.Info{Name: "net." + name + "." + item.suffix, Kind: source.KindScalar, Unit: item.unit}
			factory := func(factoryContext context.Context) (source.Collector, error) {
				return newNetworkCollector(factoryContext, read, now, interfaceName, metric)
			}
			if err := registry.RegisterAvailable(info, factory); err != nil {
				return err
			}
		}
	}
	return nil
}

func registerDisks(registry *source.Registry, read ReadFile, now func() time.Time, probe map[string]DiskCounters) error {
	names := sortedKeys(probe)
	metrics := []struct {
		suffix string
		unit   string
		metric diskMetric
	}{{"read", "B/s", diskRead}, {"write", "B/s", diskWrite}, {"ops", "ops/s", diskOperations}}
	for _, name := range names {
		for _, item := range metrics {
			device, metric := name, item.metric
			info := source.Info{Name: "io." + name + "." + item.suffix, Kind: source.KindScalar, Unit: item.unit}
			factory := func(factoryContext context.Context) (source.Collector, error) {
				return newDiskCollector(factoryContext, read, now, device, metric)
			}
			if err := registry.RegisterAvailable(info, factory); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isContextFailure(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
