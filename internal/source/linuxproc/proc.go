// Package linuxproc collects Linux memory, network, and block-device telemetry
// from procfs without exposing procfs details to consumers of source.Registry.
package linuxproc

import (
	"context"
	"fmt"
	"os"
)

const (
	MemInfoPath   = "/proc/meminfo"
	NetDevPath    = "/proc/net/dev"
	DiskStatsPath = "/proc/diskstats"
)

// ReadFile supplies one procfs observation. Tests inject fixture readers.
type ReadFile func(context.Context) ([]byte, error)

func readProcFile(ctx context.Context, path string) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func ReadMemInfo(ctx context.Context) ([]byte, error)   { return readProcFile(ctx, MemInfoPath) }
func ReadNetDev(ctx context.Context) ([]byte, error)    { return readProcFile(ctx, NetDevPath) }
func ReadDiskStats(ctx context.Context) ([]byte, error) { return readProcFile(ctx, DiskStatsPath) }

func readAndParse[T any](ctx context.Context, read ReadFile, label string, parse func([]byte) (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		return zero, fmt.Errorf("%s: context is nil", label)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if read == nil {
		return zero, fmt.Errorf("%s reader is nil", label)
	}
	data, err := read(ctx)
	if err != nil {
		return zero, err
	}
	value, err := parse(data)
	if err != nil {
		return zero, err
	}
	return value, nil
}
