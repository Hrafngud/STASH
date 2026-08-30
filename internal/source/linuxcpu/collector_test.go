package linuxcpu

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/source"
)

type sequenceReader struct {
	observations [][]byte
	index        int
}

func (reader *sequenceReader) read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reader.index >= len(reader.observations) {
		return nil, fmt.Errorf("fixture sequence exhausted")
	}
	observation := reader.observations[reader.index]
	reader.index++
	return observation, nil
}

func TestUsageCollectorsProduceAggregateCoreAndOrderedVectorSamples(t *testing.T) {
	t.Parallel()
	when := time.Unix(1234, 5678)
	tests := []struct {
		name     string
		selected selector
		wantKind source.Kind
		want     []float64
	}{
		{name: "aggregate", selected: selector{kind: selectAggregate}, wantKind: source.KindScalar, want: []float64{70.0 / 130.0 * 100}},
		{name: "core", selected: selector{kind: selectCore, core: 1}, wantKind: source.KindScalar, want: []float64{50}},
		{name: "vector", selected: selector{kind: selectCores}, wantKind: source.KindVector, want: []float64{20, 50, 100}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &sequenceReader{observations: [][]byte{
				fixture(t, "proc_stat_base.txt"),
				fixture(t, "proc_stat_next.txt"),
			}}
			collector, err := newUsageCollector(context.Background(), reader.read, func() time.Time { return when }, test.selected)
			if err != nil {
				t.Fatal(err)
			}
			sample, err := collector.Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if sample.SampleKind() != test.wantKind || sample.SampleTime() != when {
				t.Fatalf("sample metadata = %s at %s, want %s at %s", sample.SampleKind(), sample.SampleTime(), test.wantKind, when)
			}
			var got []float64
			switch typed := sample.(type) {
			case source.ScalarSample:
				got = []float64{typed.Value}
			case source.VectorSample:
				got = typed.Values
			default:
				t.Fatalf("unexpected sample type %T", sample)
			}
			if len(got) != len(test.want) {
				t.Fatalf("values = %v, want %v", got, test.want)
			}
			for index := range got {
				if difference := got[index] - test.want[index]; difference < -1e-12 || difference > 1e-12 {
					t.Fatalf("values = %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestUsageCollectorRejectsMalformedObservationWithoutSample(t *testing.T) {
	t.Parallel()
	reader := &sequenceReader{observations: [][]byte{
		fixture(t, "proc_stat_base.txt"),
		fixture(t, "proc_stat_malformed.txt"),
	}}
	collector, err := newUsageCollector(context.Background(), reader.read, time.Now, selector{kind: selectAggregate})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if sample != nil || err == nil || !strings.Contains(err.Error(), "invalid counter") {
		t.Fatalf("Collect = (%#v, %v), want nil malformed-data error", sample, err)
	}
}

func TestUsageCollectorRebaselinesAfterCounterReset(t *testing.T) {
	t.Parallel()
	reader := &sequenceReader{observations: [][]byte{
		fixture(t, "proc_stat_base.txt"),
		fixture(t, "proc_stat_reset.txt"),
		fixture(t, "proc_stat_after_reset.txt"),
	}}
	collector, err := newUsageCollector(context.Background(), reader.read, time.Now, selector{kind: selectAggregate})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if sample != nil || !errors.Is(err, ErrCounterReset) {
		t.Fatalf("reset Collect = (%#v, %v), want nil ErrCounterReset", sample, err)
	}
	sample, err = collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect after reset: %v", err)
	}
	got := sample.(source.ScalarSample).Value
	// Reset-to-recovery deltas are 8 busy ticks and 7 idle ticks.
	want := 8.0 / 15.0 * 100
	if difference := got - want; difference < -1e-12 || difference > 1e-12 {
		t.Fatalf("value after reset = %v, want %v", got, want)
	}
}

func TestVectorCollectorRejectsCoreSetChanges(t *testing.T) {
	t.Parallel()
	base := fixture(t, "proc_stat_base.txt")
	withoutCoreOne := []byte(strings.ReplaceAll(string(fixture(t, "proc_stat_next.txt")), "cpu1 30 0 50 220 30 0 0 0 0 0\n", ""))
	reader := &sequenceReader{observations: [][]byte{base, withoutCoreOne}}
	collector, err := newUsageCollector(context.Background(), reader.read, time.Now, selector{kind: selectCores})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := collector.Collect(context.Background())
	if sample != nil || err == nil || !strings.Contains(err.Error(), "logical CPU set changed") {
		t.Fatalf("Collect = (%#v, %v), want core-set error", sample, err)
	}
}

func TestCollectorHonorsCancellation(t *testing.T) {
	t.Parallel()
	reader := &sequenceReader{observations: [][]byte{fixture(t, "proc_stat_base.txt")}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newUsageCollector(ctx, reader.read, time.Now, selector{kind: selectAggregate}); !errors.Is(err, context.Canceled) {
		t.Fatalf("newUsageCollector error = %v, want context.Canceled", err)
	}
}

func TestEqualIndices(t *testing.T) {
	t.Parallel()
	if !equalIndices([]int{0, 2}, []int{0, 2}) || equalIndices([]int{0, 2}, []int{0, 1}) || equalIndices([]int{0}, []int{0, 1}) {
		t.Fatal("equalIndices returned an incorrect result")
	}
	if reflect.DeepEqual([]int{0}, []int{1}) {
		t.Fatal("sanity check failed")
	}
}
