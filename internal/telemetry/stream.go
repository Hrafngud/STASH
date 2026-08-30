// Package telemetry writes source samples using STASH's machine-oriented
// telemetry format.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/zalmo/stash/internal/source"
)

// WriteSample writes exactly one scalar or vector telemetry line. A complete
// line is formatted and validated before any bytes are passed to output.
func WriteSample(output io.Writer, sample source.Sample) error {
	if output == nil {
		return fmt.Errorf("write telemetry sample: output is nil")
	}

	line, err := formatSample(sample)
	if err != nil {
		return fmt.Errorf("write telemetry sample: %w", err)
	}
	written, err := output.Write(line)
	if err != nil {
		return fmt.Errorf("write telemetry sample: %w", err)
	}
	if written != len(line) {
		return fmt.Errorf("write telemetry sample: %w", io.ErrShortWrite)
	}
	return nil
}

// Stream collects and writes samples until clean source exhaustion. A zero
// interval collects immediately, which lets stdin retain the producer's
// cadence. A positive interval waits before every observation, which supports
// polled telemetry sources without a busy loop. All errors are returned to the
// caller and are never written to output.
func Stream(ctx context.Context, output io.Writer, collector source.Collector, interval time.Duration) error {
	if ctx == nil {
		return fmt.Errorf("stream telemetry: context is nil")
	}
	if output == nil {
		return fmt.Errorf("stream telemetry: output is nil")
	}
	if collector == nil {
		return fmt.Errorf("stream telemetry: collector is nil")
	}
	if interval < 0 {
		return fmt.Errorf("stream telemetry: interval must not be negative")
	}

	var ticker *time.Ticker
	if interval > 0 {
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	for {
		if err := waitForObservation(ctx, ticker); err != nil {
			return fmt.Errorf("stream telemetry: %w", err)
		}

		sample, err := collector.Collect(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream telemetry: collect sample: %w", err)
		}
		if err := WriteSample(output, sample); err != nil {
			return fmt.Errorf("stream telemetry: %w", err)
		}
	}
}

func waitForObservation(ctx context.Context, ticker *time.Ticker) error {
	if ticker == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
		return nil
	}
}

func formatSample(sample source.Sample) ([]byte, error) {
	var values []float64
	switch typed := sample.(type) {
	case source.ScalarSample:
		values = []float64{typed.Value}
	case *source.ScalarSample:
		if typed == nil {
			return nil, fmt.Errorf("sample is nil")
		}
		values = []float64{typed.Value}
	case source.VectorSample:
		values = typed.Values
	case *source.VectorSample:
		if typed == nil {
			return nil, fmt.Errorf("sample is nil")
		}
		values = typed.Values
	case nil:
		return nil, fmt.Errorf("sample is nil")
	default:
		return nil, fmt.Errorf("unsupported sample type %T", sample)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("sample has no values")
	}

	line := make([]byte, 0, len(values)*16)
	for index, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("sample value %d is not finite", index)
		}
		if index > 0 {
			line = append(line, ',')
		}
		// Fixed-point output stays within STASH's decimal input grammar, so a
		// telemetry stream can be piped back into the stdin source.
		line = strconv.AppendFloat(line, value, 'f', -1, 64)
	}
	return append(line, '\n'), nil
}
