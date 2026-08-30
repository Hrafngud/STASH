package stdin

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/source"
)

func TestCollectorReadsOneScalarPerNonEmptyLine(t *testing.T) {
	t.Parallel()
	when := time.Unix(123, 456)
	collector, err := New(strings.NewReader("\n0\n.25\n\n-1.5k\n1G"), func() time.Time { return when })
	if err != nil {
		t.Fatal(err)
	}

	for index, want := range []float64{0, 0.25, -1500, 1_000_000_000} {
		sample, err := collector.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect sample %d: %v", index, err)
		}
		scalar, ok := sample.(source.ScalarSample)
		if !ok {
			t.Fatalf("Collect sample %d returned %T, want source.ScalarSample", index, sample)
		}
		if scalar.Value != want || scalar.Time != when {
			t.Fatalf("Collect sample %d = %#v, want value %v at %s", index, scalar, want, when)
		}
	}
	if sample, err := collector.Collect(context.Background()); sample != nil || !errors.Is(err, io.EOF) {
		t.Fatalf("Collect at EOF = (%#v, %v), want (nil, io.EOF)", sample, err)
	}
}

func TestCollectorReportsInvalidNonEmptyLineNumber(t *testing.T) {
	t.Parallel()
	collector, err := New(strings.NewReader("\n1\n\n  \n2\n"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}

	sample, firstErr := collector.Collect(context.Background())
	if sample != nil || firstErr == nil {
		t.Fatalf("invalid Collect = (%#v, %v), want error", sample, firstErr)
	}
	if got := firstErr.Error(); !strings.Contains(got, "stdin line 4") || !strings.Contains(got, "invalid number") {
		t.Fatalf("invalid-line error = %q, want line number and numeric context", got)
	}
	if _, repeatedErr := collector.Collect(context.Background()); repeatedErr != firstErr {
		t.Fatalf("Collect after invalid line error = %v, want original terminal error %v", repeatedErr, firstErr)
	}
}

func TestCollectorHandlesCRLFAndFinalLineWithoutNewline(t *testing.T) {
	t.Parallel()
	collector, err := New(strings.NewReader("1\r\n\r\n2"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []float64{1, 2} {
		sample, err := collector.Collect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got := sample.(source.ScalarSample).Value; got != want {
			t.Fatalf("sample value = %v, want %v", got, want)
		}
	}
}

func TestCollectorHonorsCancellationWithoutProducingSample(t *testing.T) {
	t.Parallel()
	collector, err := New(strings.NewReader("1\n"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sample, err := collector.Collect(ctx); sample != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Collect canceled = (%#v, %v), want (nil, context.Canceled)", sample, err)
	}

	sample, err := collector.Collect(context.Background())
	if err != nil || sample.(source.ScalarSample).Value != 1 {
		t.Fatalf("Collect after cancellation = (%#v, %v), want value 1", sample, err)
	}
}

func TestCollectorValidatesConstructionAndContext(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, time.Now); err == nil || !strings.Contains(err.Error(), "reader is nil") {
		t.Fatalf("New(nil) error = %v", err)
	}
	collector, err := New(strings.NewReader("1\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if sample, err := collector.Collect(nil); sample != nil || err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("Collect(nil) = (%#v, %v), want context error", sample, err)
	}
	var nilCollector *Collector
	if sample, err := nilCollector.Collect(context.Background()); sample != nil || err == nil {
		t.Fatalf("nil collector Collect = (%#v, %v), want error", sample, err)
	}
}

func TestRegisterAddsIndependentlyOpenedStdinSource(t *testing.T) {
	t.Parallel()
	registry := source.NewRegistry()
	opens := 0
	open := func(ctx context.Context) (io.Reader, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opens++
		return strings.NewReader("7\n"), nil
	}
	if err := Register(registry, open, time.Now); err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Lookup(Name)
	if !ok || !entry.Available || entry.Info != Info() {
		t.Fatalf("registered entry = %#v, %v", entry, ok)
	}
	for index := 0; index < 2; index++ {
		collector, err := registry.NewCollector(context.Background(), Name)
		if err != nil {
			t.Fatal(err)
		}
		sample, err := collector.Collect(context.Background())
		if err != nil || sample.(source.ScalarSample).Value != 7 {
			t.Fatalf("collector %d sample = (%#v, %v)", index, sample, err)
		}
	}
	if opens != 2 {
		t.Fatalf("reader opens = %d, want 2", opens)
	}
}

func TestRegisterValidatesAndWrapsOpenerFailures(t *testing.T) {
	t.Parallel()
	if err := Register(nil, func(context.Context) (io.Reader, error) { return strings.NewReader(""), nil }, time.Now); err == nil {
		t.Fatal("Register accepted nil registry")
	}
	registry := source.NewRegistry()
	if err := Register(registry, nil, time.Now); err == nil {
		t.Fatal("Register accepted nil opener")
	}

	openErr := errors.New("input unavailable")
	registry = source.NewRegistry()
	if err := Register(registry, func(context.Context) (io.Reader, error) { return nil, openErr }, time.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.NewCollector(context.Background(), Name); !errors.Is(err, openErr) {
		t.Fatalf("NewCollector error = %v, want wrapped opener error", err)
	}
}
