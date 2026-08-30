// Package source defines telemetry metadata, samples, collectors, and source
// registration independently of any operating-system backend.
package source

import (
	"context"
	"time"
)

// DefaultSampleInterval is the default 20 Hz telemetry sampling interval.
const DefaultSampleInterval = 50 * time.Millisecond

// Kind distinguishes scalar controls from ordered vector controls.
type Kind string

const (
	KindScalar Kind = "scalar"
	KindVector Kind = "vector"
)

// Info is the stable public metadata for a telemetry source.
type Info struct {
	Name       string
	Kind       Kind
	Unit       string
	NaturalMin *float64
	NaturalMax *float64
}

// Sample is a timestamped scalar or vector telemetry observation.
type Sample interface {
	SampleKind() Kind
	SampleTime() time.Time
}

// ScalarSample is one scalar telemetry observation.
type ScalarSample struct {
	Value float64
	Time  time.Time
}

func (sample ScalarSample) SampleKind() Kind      { return KindScalar }
func (sample ScalarSample) SampleTime() time.Time { return sample.Time }

// VectorSample is one ordered vector telemetry observation. Values retain the
// stable ordering documented by the source metadata.
type VectorSample struct {
	Values []float64
	Time   time.Time
}

func (sample VectorSample) SampleKind() Kind      { return KindVector }
func (sample VectorSample) SampleTime() time.Time { return sample.Time }

// Collector produces samples for one registered source. Scheduling belongs to
// the runtime control layer; collectors perform exactly one observation per
// call.
type Collector interface {
	Collect(context.Context) (Sample, error)
}

// Factory constructs an independently stateful collector. Delta-based sources
// may read an initial baseline while the factory runs.
type Factory func(context.Context) (Collector, error)
