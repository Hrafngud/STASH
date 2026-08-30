package telemetry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/source"
	stdinsource "github.com/zalmo/stash/internal/source/stdin"
)

func TestWriteSampleUsesExactMachineFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sample source.Sample
		want   string
	}{
		{name: "scalar", sample: source.ScalarSample{Value: 42.7}, want: "42.7\n"},
		{name: "scalar integer", sample: &source.ScalarSample{Value: 100}, want: "100\n"},
		{name: "large scalar remains plain decimal", sample: source.ScalarSample{Value: 3e9}, want: "3000000000\n"},
		{name: "small scalar remains plain decimal", sample: source.ScalarSample{Value: 1e-7}, want: "0.0000001\n"},
		{name: "vector", sample: source.VectorSample{Values: []float64{12.1, 100, 7.2, 83.4}}, want: "12.1,100,7.2,83.4\n"},
		{name: "vector pointer", sample: &source.VectorSample{Values: []float64{-1, 0, 1}}, want: "-1,0,1\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteSample(&output, test.sample); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != test.want {
				t.Fatalf("output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWriteSampleRejectsInvalidSamplesBeforeWriting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sample source.Sample
	}{
		{name: "nil", sample: nil},
		{name: "nil scalar", sample: (*source.ScalarSample)(nil)},
		{name: "empty vector", sample: source.VectorSample{}},
		{name: "NaN scalar", sample: source.ScalarSample{Value: math.NaN()}},
		{name: "infinite vector", sample: source.VectorSample{Values: []float64{1, math.Inf(1), 2}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteSample(&output, test.sample); err == nil {
				t.Fatalf("WriteSample(%#v) unexpectedly succeeded", test.sample)
			}
			if output.Len() != 0 {
				t.Fatalf("invalid sample wrote %q", output.String())
			}
		})
	}
}

type sequenceCollector struct {
	samples []source.Sample
	err     error
	index   int
}

func (collector *sequenceCollector) Collect(ctx context.Context) (source.Sample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if collector.index < len(collector.samples) {
		sample := collector.samples[collector.index]
		collector.index++
		return sample, nil
	}
	if collector.err != nil {
		return nil, collector.err
	}
	return nil, io.EOF
}

func TestStreamWritesSamplesUntilCleanExhaustion(t *testing.T) {
	t.Parallel()
	collector := &sequenceCollector{samples: []source.Sample{
		source.ScalarSample{Value: 1},
		source.VectorSample{Values: []float64{2, 3}},
	}}
	var output bytes.Buffer
	if err := Stream(context.Background(), &output, collector, 0); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "1\n2,3\n"; got != want {
		t.Fatalf("stream output = %q, want %q", got, want)
	}
}

func TestStreamFromStdinPreservesNumericStdoutAndReturnsLineError(t *testing.T) {
	t.Parallel()
	collector, err := stdinsource.New(strings.NewReader("1\n\ninvalid\n2\n"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Stream(context.Background(), &output, collector, 0)
	if err == nil || !strings.Contains(err.Error(), "stdin line 3") {
		t.Fatalf("Stream error = %v, want stdin line 3 error", err)
	}
	if got := output.String(); got != "1\n" {
		t.Fatalf("stream stdout = %q, want only valid numeric samples", got)
	}
}

func TestStreamReturnsRuntimeErrorWithoutWritingDiagnostic(t *testing.T) {
	t.Parallel()
	runtimeErr := errors.New("source disappeared")
	collector := &sequenceCollector{samples: []source.Sample{source.ScalarSample{Value: 1}}, err: runtimeErr}
	var output bytes.Buffer
	err := Stream(context.Background(), &output, collector, 0)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Stream error = %v, want wrapped runtime error", err)
	}
	if got := output.String(); got != "1\n" {
		t.Fatalf("stream stdout = %q, want only numeric sample", got)
	}
	if strings.Contains(output.String(), runtimeErr.Error()) {
		t.Fatalf("runtime diagnostic leaked to stdout: %q", output.String())
	}
}

func TestStreamReturnsCancellationWithoutOutput(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := &sequenceCollector{samples: []source.Sample{source.ScalarSample{Value: 1}}}
	var output bytes.Buffer
	err := Stream(ctx, &output, collector, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stream error = %v, want context.Canceled", err)
	}
	if output.Len() != 0 || collector.index != 0 {
		t.Fatalf("canceled stream wrote %q after %d collections", output.String(), collector.index)
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestStreamAndWriterValidation(t *testing.T) {
	t.Parallel()
	collector := &sequenceCollector{}
	if err := Stream(nil, io.Discard, collector, 0); err == nil {
		t.Fatal("Stream accepted nil context")
	}
	if err := Stream(context.Background(), nil, collector, 0); err == nil {
		t.Fatal("Stream accepted nil output")
	}
	if err := Stream(context.Background(), io.Discard, nil, 0); err == nil {
		t.Fatal("Stream accepted nil collector")
	}
	if err := Stream(context.Background(), io.Discard, collector, -time.Second); err == nil {
		t.Fatal("Stream accepted negative interval")
	}
	if err := WriteSample(nil, source.ScalarSample{Value: 1}); err == nil {
		t.Fatal("WriteSample accepted nil output")
	}
	if err := WriteSample(shortWriter{}, source.ScalarSample{Value: 1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short WriteSample error = %v, want io.ErrShortWrite", err)
	}
}

func TestStreamPositiveIntervalWaitsBeforeCollection(t *testing.T) {
	t.Parallel()
	collector := &sequenceCollector{samples: []source.Sample{source.ScalarSample{Value: 1}}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	if err := Stream(ctx, &output, collector, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "1\n" {
		t.Fatalf("stream output = %q, want one scheduled sample", got)
	}
}
