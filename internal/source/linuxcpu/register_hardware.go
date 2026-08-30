package linuxcpu

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
	AggregateFrequencyName = "cpu.freq"
	CoresFrequencyName     = "cpu.cores.freq"
	TemperatureName        = "cpu.temp"
	PowerName              = "cpu.power"
)

const unavailablePowerReason = "no reliable local CPU energy or power interface implemented"

// RegisterDefaultHardware detects Linux CPU frequency and temperature through
// sysfs and explicitly records CPU power as unavailable.
func RegisterDefaultHardware(ctx context.Context, registry *source.Registry) error {
	return RegisterHardware(ctx, registry, os.DirFS("/"), time.Now)
}

// RegisterHardware detects and registers CPU frequency, temperature, and
// power availability. The filesystem and clock are injected for fixture-based
// tests; filesystem paths are rooted as they are under Linux /sys.
func RegisterHardware(ctx context.Context, registry *source.Registry, filesystem fs.FS, now func() time.Time) error {
	if registry == nil {
		return fmt.Errorf("register CPU hardware sources: registry is nil")
	}
	if err := checkHardwareInputs(ctx, filesystem); err != nil {
		return fmt.Errorf("register CPU hardware sources: %w", err)
	}
	if now == nil {
		now = time.Now
	}

	frequency, frequencyErr := detectFrequency(ctx, filesystem)
	if isContextError(frequencyErr) {
		return fmt.Errorf("register CPU hardware sources: %w", frequencyErr)
	}
	temperature, temperatureErr := detectTemperature(ctx, filesystem)
	if isContextError(temperatureErr) {
		return fmt.Errorf("register CPU hardware sources: %w", temperatureErr)
	}

	if err := registerFrequency(registry, filesystem, now, frequency, frequencyErr); err != nil {
		return err
	}
	if err := registerTemperature(registry, filesystem, now, temperature, temperatureErr); err != nil {
		return err
	}
	if err := registry.RegisterUnavailable(powerInfo(), unavailablePowerReason); err != nil {
		return err
	}
	return nil
}

func registerFrequency(registry *source.Registry, filesystem fs.FS, now func() time.Time, probe frequencyProbe, detectionErr error) error {
	if detectionErr != nil {
		reason := detectionErr.Error()
		if err := registry.RegisterUnavailable(frequencyInfo(AggregateFrequencyName, source.KindScalar, nil, nil), reason); err != nil {
			return err
		}
		return registry.RegisterUnavailable(frequencyInfo(CoresFrequencyName, source.KindVector, nil, nil), reason)
	}

	aggregateMinimum, aggregateMaximum := aggregateFrequencyRange(probe.cores)
	vectorMinimum, vectorMaximum := vectorFrequencyRange(probe.cores)
	register := func(info source.Info, selected selector) error {
		cores := append([]frequencyCore(nil), probe.cores...)
		factory := func(factoryContext context.Context) (source.Collector, error) {
			if err := checkHardwareInputs(factoryContext, filesystem); err != nil {
				return nil, err
			}
			return &frequencyCollector{filesystem: filesystem, cores: cores, selected: selected, now: now}, nil
		}
		return registry.RegisterAvailable(info, factory)
	}
	if err := register(frequencyInfo(AggregateFrequencyName, source.KindScalar, aggregateMinimum, aggregateMaximum), selector{kind: selectAggregate}); err != nil {
		return err
	}
	for _, core := range probe.cores {
		name := fmt.Sprintf("cpu.core.%d.freq", core.index)
		if err := register(frequencyInfo(name, source.KindScalar, core.minimumHz, core.maximumHz), selector{kind: selectCore, core: core.index}); err != nil {
			return err
		}
	}
	return register(frequencyInfo(CoresFrequencyName, source.KindVector, vectorMinimum, vectorMaximum), selector{kind: selectCores})
}

func registerTemperature(registry *source.Registry, filesystem fs.FS, now func() time.Time, probe temperatureProbe, detectionErr error) error {
	info := source.Info{Name: TemperatureName, Kind: source.KindScalar, Unit: "°C"}
	if detectionErr != nil {
		return registry.RegisterUnavailable(info, detectionErr.Error())
	}
	paths := append([]string(nil), probe.paths...)
	factory := func(factoryContext context.Context) (source.Collector, error) {
		if err := checkHardwareInputs(factoryContext, filesystem); err != nil {
			return nil, err
		}
		return &temperatureCollector{filesystem: filesystem, paths: paths, now: now}, nil
	}
	return registry.RegisterAvailable(info, factory)
}

func frequencyInfo(name string, kind source.Kind, minimum, maximum *float64) source.Info {
	return source.Info{
		Name:       name,
		Kind:       kind,
		Unit:       "Hz",
		NaturalMin: cloneFloatPointer(minimum),
		NaturalMax: cloneFloatPointer(maximum),
	}
}

func aggregateFrequencyRange(cores []frequencyCore) (*float64, *float64) {
	if len(cores) == 0 {
		return nil, nil
	}
	if cores[0].minimumHz == nil || cores[0].maximumHz == nil {
		return nil, nil
	}
	minimums := make([]float64, len(cores))
	maximums := make([]float64, len(cores))
	for index, core := range cores {
		if core.minimumHz == nil || core.maximumHz == nil {
			return nil, nil
		}
		minimums[index] = *core.minimumHz
		maximums[index] = *core.maximumHz
	}
	return floatPointer(average(minimums)), floatPointer(average(maximums))
}

func vectorFrequencyRange(cores []frequencyCore) (*float64, *float64) {
	if len(cores) == 0 || cores[0].minimumHz == nil || cores[0].maximumHz == nil {
		return nil, nil
	}
	minimum, maximum := *cores[0].minimumHz, *cores[0].maximumHz
	for _, core := range cores[1:] {
		if core.minimumHz == nil || core.maximumHz == nil {
			return nil, nil
		}
		if *core.minimumHz < minimum {
			minimum = *core.minimumHz
		}
		if *core.maximumHz > maximum {
			maximum = *core.maximumHz
		}
	}
	return floatPointer(minimum), floatPointer(maximum)
}

func powerInfo() source.Info {
	return source.Info{Name: PowerName, Kind: source.KindScalar, Unit: "W"}
}

func cloneFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return floatPointer(*value)
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
