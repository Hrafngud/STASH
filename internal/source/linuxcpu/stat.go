// Package linuxcpu collects Linux CPU telemetry without exposing procfs details
// to the source registry or audio layers.
package linuxcpu

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	// ErrCounterReset means at least one cumulative kernel counter moved
	// backwards, so no delta sample can be produced safely.
	ErrCounterReset = errors.New("CPU counters reset or wrapped")
	// ErrNoProgress means the counters did not advance between observations.
	ErrNoProgress = errors.New("CPU counters did not advance")
)

// Counters contains the /proc/stat fields used for CPU utilization. Guest
// fields are deliberately excluded because Linux already includes them in user
// and nice time.
type Counters struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
}

func (counters Counters) values() [8]uint64 {
	return [8]uint64{
		counters.User,
		counters.Nice,
		counters.System,
		counters.Idle,
		counters.IOWait,
		counters.IRQ,
		counters.SoftIRQ,
		counters.Steal,
	}
}

// Snapshot is one parsed aggregate and per-core /proc/stat observation.
type Snapshot struct {
	aggregate Counters
	cores     map[int]Counters
	indices   []int
}

func (snapshot Snapshot) Aggregate() Counters { return snapshot.aggregate }

func (snapshot Snapshot) Core(index int) (Counters, bool) {
	counters, ok := snapshot.cores[index]
	return counters, ok
}

// CoreIndices returns a new ascending list of logical CPU indices.
func (snapshot Snapshot) CoreIndices() []int {
	return append([]int(nil), snapshot.indices...)
}

// ParseStat parses aggregate and logical-core CPU counter lines from
// /proc/stat. Non-CPU kernel statistics are ignored.
func ParseStat(data []byte) (Snapshot, error) {
	snapshot := Snapshot{cores: make(map[int]Counters)}
	hasAggregate := false

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}

		label := fields[0]
		isAggregate := label == "cpu"
		coreIndex := -1
		if !isAggregate {
			suffix := strings.TrimPrefix(label, "cpu")
			if suffix == "" || !allDecimalDigits(suffix) {
				return Snapshot{}, fmt.Errorf("parse /proc/stat line %d: invalid CPU label %q", lineNumber, label)
			}
			parsedIndex, err := strconv.ParseInt(suffix, 10, 32)
			if err != nil {
				return Snapshot{}, fmt.Errorf("parse /proc/stat line %d: invalid CPU index %q: %w", lineNumber, suffix, err)
			}
			coreIndex = int(parsedIndex)
		}

		counters, err := parseCounters(fields[1:])
		if err != nil {
			return Snapshot{}, fmt.Errorf("parse /proc/stat line %d (%s): %w", lineNumber, label, err)
		}
		if isAggregate {
			if hasAggregate {
				return Snapshot{}, fmt.Errorf("parse /proc/stat line %d: duplicate aggregate CPU line", lineNumber)
			}
			snapshot.aggregate = counters
			hasAggregate = true
			continue
		}
		if _, duplicate := snapshot.cores[coreIndex]; duplicate {
			return Snapshot{}, fmt.Errorf("parse /proc/stat line %d: duplicate CPU core %d", lineNumber, coreIndex)
		}
		snapshot.cores[coreIndex] = counters
		snapshot.indices = append(snapshot.indices, coreIndex)
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("scan /proc/stat: %w", err)
	}
	if !hasAggregate {
		return Snapshot{}, fmt.Errorf("parse /proc/stat: aggregate CPU line is missing")
	}
	if len(snapshot.cores) == 0 {
		return Snapshot{}, fmt.Errorf("parse /proc/stat: per-core CPU lines are missing")
	}
	sortInts(snapshot.indices)
	return snapshot, nil
}

func parseCounters(fields []string) (Counters, error) {
	if len(fields) < 4 {
		return Counters{}, fmt.Errorf("expected at least 4 counters, got %d", len(fields))
	}
	values := [8]uint64{}
	for index, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return Counters{}, fmt.Errorf("invalid counter %d %q: %w", index+1, field, err)
		}
		if index < len(values) {
			values[index] = value
		}
	}
	return Counters{
		User: values[0], Nice: values[1], System: values[2], Idle: values[3],
		IOWait: values[4], IRQ: values[5], SoftIRQ: values[6], Steal: values[7],
	}, nil
}

// Utilization computes busy CPU percentage from two cumulative counter sets.
func Utilization(previous, current Counters) (float64, error) {
	previousValues := previous.values()
	currentValues := current.values()
	var totalDelta uint64
	var idleDelta uint64
	for index := range previousValues {
		if currentValues[index] < previousValues[index] {
			return 0, fmt.Errorf("%w: counter %d moved from %d to %d", ErrCounterReset, index+1, previousValues[index], currentValues[index])
		}
		delta := currentValues[index] - previousValues[index]
		if math.MaxUint64-totalDelta < delta {
			return 0, fmt.Errorf("%w: total delta overflow", ErrCounterReset)
		}
		totalDelta += delta
		if index == 3 || index == 4 {
			if math.MaxUint64-idleDelta < delta {
				return 0, fmt.Errorf("%w: idle delta overflow", ErrCounterReset)
			}
			idleDelta += delta
		}
	}
	if totalDelta == 0 {
		return 0, ErrNoProgress
	}
	busyDelta := totalDelta - idleDelta
	return float64(busyDelta) / float64(totalDelta) * 100, nil
}

func allDecimalDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func sortInts(values []int) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
