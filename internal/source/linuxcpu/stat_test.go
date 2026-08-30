package linuxcpu

import (
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseStatFixtureAndStableCoreOrder(t *testing.T) {
	t.Parallel()
	snapshot, err := ParseStat(fixture(t, "proc_stat_base.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot.CoreIndices(), []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CoreIndices() = %v, want %v", got, want)
	}
	if snapshot.Aggregate().User != 100 || snapshot.Aggregate().IOWait != 100 {
		t.Fatalf("aggregate counters = %#v", snapshot.Aggregate())
	}
	core, ok := snapshot.Core(2)
	if !ok || core.User != 30 || core.Idle != 200 {
		t.Fatalf("core 2 = %#v, %v", core, ok)
	}
	indices := snapshot.CoreIndices()
	indices[0] = 99
	if snapshot.CoreIndices()[0] != 0 {
		t.Fatal("CoreIndices exposed mutable snapshot ordering")
	}
}

func TestParseStatRejectsMalformedKernelData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "fixture bad counter", data: string(fixture(t, "proc_stat_malformed.txt")), want: "invalid counter"},
		{name: "missing aggregate", data: "cpu0 1 2 3 4\n", want: "aggregate CPU line is missing"},
		{name: "missing cores", data: "cpu 1 2 3 4\n", want: "per-core CPU lines are missing"},
		{name: "short counters", data: "cpu 1 2 3\ncpu0 1 2 3 4\n", want: "at least 4 counters"},
		{name: "invalid label", data: "cpu 1 2 3 4\ncpux 1 2 3 4\n", want: "invalid CPU label"},
		{name: "duplicate aggregate", data: "cpu 1 2 3 4\ncpu 1 2 3 4\ncpu0 1 2 3 4\n", want: "duplicate aggregate"},
		{name: "duplicate core", data: "cpu 1 2 3 4\ncpu0 1 2 3 4\ncpu0 1 2 3 4\n", want: "duplicate CPU core"},
		{name: "negative", data: "cpu 1 2 3 4\ncpu0 -1 2 3 4\n", want: "invalid counter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseStat([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseStat error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestUtilization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		previous Counters
		current  Counters
		want     float64
		wantErr  error
	}{
		{
			name:     "half busy",
			previous: Counters{User: 10, System: 10, Idle: 80},
			current:  Counters{User: 30, System: 20, Idle: 110},
			want:     50,
		},
		{
			name:     "iowait counts idle",
			previous: Counters{},
			current:  Counters{User: 25, Idle: 25, IOWait: 50},
			want:     25,
		},
		{
			name:     "fully busy",
			previous: Counters{Idle: 100},
			current:  Counters{User: 10, System: 10, Idle: 100},
			want:     100,
		},
		{
			name:     "no progress",
			previous: Counters{User: 10},
			current:  Counters{User: 10},
			wantErr:  ErrNoProgress,
		},
		{
			name:     "counter reset",
			previous: Counters{User: 10, Idle: 100},
			current:  Counters{User: 9, Idle: 110},
			wantErr:  ErrCounterReset,
		},
		{
			name:     "delta overflow",
			previous: Counters{},
			current:  Counters{User: math.MaxUint64, System: 1},
			wantErr:  ErrCounterReset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Utilization(test.previous, test.current)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Utilization error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && math.Abs(got-test.want) > 1e-12 {
				t.Fatalf("Utilization = %v, want %v", got, test.want)
			}
		})
	}
}
