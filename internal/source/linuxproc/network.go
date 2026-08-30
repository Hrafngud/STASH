package linuxproc

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/source"
)

type NetworkCounters struct {
	ReceiveBytes    uint64
	ReceivePackets  uint64
	TransmitBytes   uint64
	TransmitPackets uint64
}

func ParseNetDev(data []byte) (map[string]NetworkCounters, error) {
	interfaces := make(map[string]NetworkCounters)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		separator := strings.IndexByte(line, ':')
		if separator < 0 {
			continue
		}
		name := strings.TrimSpace(line[:separator])
		if name == "" || strings.ContainsAny(name, " \t\r\n:") {
			return nil, fmt.Errorf("parse /proc/net/dev line %d: invalid interface name %q", lineNumber, name)
		}
		if _, duplicate := interfaces[name]; duplicate {
			return nil, fmt.Errorf("parse /proc/net/dev line %d: duplicate interface %q", lineNumber, name)
		}
		fields := strings.Fields(line[separator+1:])
		if len(fields) < 16 {
			return nil, fmt.Errorf("parse /proc/net/dev line %d (%s): expected at least 16 counters, got %d", lineNumber, name, len(fields))
		}
		indices := [...]int{0, 1, 8, 9}
		values := [4]uint64{}
		for index, fieldIndex := range indices {
			value, err := strconv.ParseUint(fields[fieldIndex], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse /proc/net/dev line %d (%s): invalid counter %d %q", lineNumber, name, fieldIndex+1, fields[fieldIndex])
			}
			values[index] = value
		}
		interfaces[name] = NetworkCounters{values[0], values[1], values[2], values[3]}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/net/dev: %w", err)
	}
	if len(interfaces) == 0 {
		return nil, fmt.Errorf("parse /proc/net/dev: no interfaces found")
	}
	return interfaces, nil
}

type networkMetric uint8

const (
	networkReceiveBytes networkMetric = iota
	networkTransmitBytes
	networkReceivePackets
	networkTransmitPackets
)

func (metric networkMetric) value(counters NetworkCounters) uint64 {
	switch metric {
	case networkReceiveBytes:
		return counters.ReceiveBytes
	case networkTransmitBytes:
		return counters.TransmitBytes
	case networkReceivePackets:
		return counters.ReceivePackets
	default:
		return counters.TransmitPackets
	}
}

type networkCollector struct {
	read          ReadFile
	now           func() time.Time
	interfaceName string
	metric        networkMetric
	previous      NetworkCounters
	previousAt    time.Time
}

func newNetworkCollector(ctx context.Context, read ReadFile, now func() time.Time, name string, metric networkMetric) (*networkCollector, error) {
	snapshot, err := readAndParse(ctx, read, "network", ParseNetDev)
	if err != nil {
		return nil, fmt.Errorf("read network baseline: %w", err)
	}
	counters, ok := snapshot[name]
	if !ok {
		return nil, fmt.Errorf("network interface %s is unavailable", name)
	}
	return &networkCollector{read: read, now: now, interfaceName: name, metric: metric, previous: counters, previousAt: now()}, nil
}

func (collector *networkCollector) Collect(ctx context.Context) (source.Sample, error) {
	snapshot, err := readAndParse(ctx, collector.read, "network", ParseNetDev)
	if err != nil {
		return nil, fmt.Errorf("collect network interface %s: %w", collector.interfaceName, err)
	}
	current, ok := snapshot[collector.interfaceName]
	if !ok {
		return nil, fmt.Errorf("collect network interface %s: interface disappeared", collector.interfaceName)
	}
	when := collector.now()
	value, err := counterRate(collector.metric.value(collector.previous), collector.metric.value(current), collector.previousAt, when, 1)
	if err != nil {
		if isCounterReset(err) {
			collector.previous, collector.previousAt = current, when
		}
		return nil, fmt.Errorf("collect network interface %s: %w", collector.interfaceName, err)
	}
	collector.previous, collector.previousAt = current, when
	return source.ScalarSample{Value: value, Time: when}, nil
}
