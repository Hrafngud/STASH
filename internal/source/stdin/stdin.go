// Package stdin implements the scalar source read from standard input.
package stdin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zalmo/stash/internal/source"
	"github.com/zalmo/stash/internal/unit"
)

const (
	// Name is the canonical source name for standard input.
	Name = "-"

	maximumLineBytes = 1024 * 1024
)

// Collector reads one numeric scalar sample from each non-empty input line.
type Collector struct {
	scanner  *bufio.Scanner
	now      func() time.Time
	line     int
	terminal error
}

// OpenReader opens an independent input stream for a collector created by a
// source registry.
type OpenReader func(context.Context) (io.Reader, error)

// Info returns the metadata for the standard-input source. Its values are
// unitless and have no natural range.
func Info() source.Info {
	return source.Info{Name: Name, Kind: source.KindScalar, Unit: "unitless"}
}

// New constructs a standard-input collector. The clock is optional and is
// injected by tests and callers that need deterministic timestamps.
func New(reader io.Reader, now func() time.Time) (*Collector, error) {
	if reader == nil {
		return nil, fmt.Errorf("open stdin source: reader is nil")
	}
	if now == nil {
		now = time.Now
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maximumLineBytes)
	return &Collector{scanner: scanner, now: now}, nil
}

// Register adds the standard-input source to a source registry. The opener is
// called for each collector so registry factories remain independently
// stateful.
func Register(registry *source.Registry, open OpenReader, now func() time.Time) error {
	if registry == nil {
		return fmt.Errorf("register stdin source: registry is nil")
	}
	if open == nil {
		return fmt.Errorf("register stdin source: reader opener is nil")
	}
	factory := func(ctx context.Context) (source.Collector, error) {
		if ctx == nil {
			return nil, fmt.Errorf("open stdin: context is nil")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reader, err := open(ctx)
		if err != nil {
			return nil, fmt.Errorf("open stdin: %w", err)
		}
		collector, err := New(reader, now)
		if err != nil {
			return nil, err
		}
		return collector, nil
	}
	return registry.RegisterAvailable(Info(), factory)
}

// Collect returns the next non-empty line as a scalar sample. Clean input
// exhaustion is reported as io.EOF. A malformed line permanently terminates
// the collector so execution cannot silently continue past bad input.
func (collector *Collector) Collect(ctx context.Context) (source.Sample, error) {
	if collector == nil {
		return nil, fmt.Errorf("collect stdin: collector is nil")
	}
	if ctx == nil {
		return nil, fmt.Errorf("collect stdin: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if collector.terminal != nil {
		return nil, collector.terminal
	}

	for collector.scanner.Scan() {
		collector.line++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		input := collector.scanner.Text()
		if input == "" {
			continue
		}

		value, err := unit.ParseNumber(input)
		if err != nil {
			collector.terminal = fmt.Errorf("stdin line %d: %w", collector.line, err)
			return nil, collector.terminal
		}
		return source.ScalarSample{Value: value, Time: collector.now()}, nil
	}

	if err := collector.scanner.Err(); err != nil {
		collector.terminal = fmt.Errorf("stdin line %d: read sample: %w", collector.line+1, err)
	} else {
		collector.terminal = io.EOF
	}
	return nil, collector.terminal
}
