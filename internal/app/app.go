// Package app wires STASH's parser, source registry, execution modes, and
// audio backend without owning process-level signal or exit-code handling.
package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/audio/csound"
	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/discovery"
	stashruntime "github.com/zalmo/stash/internal/runtime"
	"github.com/zalmo/stash/internal/source"
	"github.com/zalmo/stash/internal/source/linuxcpu"
	"github.com/zalmo/stash/internal/source/linuxgpu"
	"github.com/zalmo/stash/internal/source/linuxproc"
	stdinsource "github.com/zalmo/stash/internal/source/stdin"
	"github.com/zalmo/stash/internal/telemetry"
	"github.com/zalmo/stash/internal/tui"
)

// Runner contains the process-independent dependencies for one command.
// Tests may inject finite sources and a fake backend; production uses
// NewDefault.
type Runner struct {
	Registry       *source.Registry
	Backend        audio.Backend
	SampleInterval func(string) time.Duration
	RhythmInterval time.Duration
	MaxDelay       time.Duration
}

// RunInteractive opens the no-argument live instrument editor. Its clauses
// are parsed and planned by the same cli package used by Run.
func (runner *Runner) RunInteractive(ctx context.Context, input io.Reader, output, diagnostics io.Writer) error {
	if runner == nil || runner.Registry == nil {
		return fmt.Errorf("execute interactive: runner or source registry is nil")
	}
	command, exported, err := tui.Run(ctx, tui.Config{
		Registry: runner.Registry, Backend: runner.Backend, Input: input, Output: output, Diagnostics: diagnostics,
		SampleInterval: runner.SampleInterval, RhythmInterval: runner.RhythmInterval, MaxDelay: runner.MaxDelay,
	})
	if err != nil {
		return err
	}
	if exported {
		_, err = fmt.Fprintln(output, command)
		return err
	}
	return nil
}

// NewDefault detects the local Linux telemetry sources, registers stdin, and
// configures the Csound backend. Unsupported hardware remains discoverable as
// unavailable when its source package has a canonical name to register.
func NewDefault(ctx context.Context, input io.Reader) (*Runner, error) {
	if ctx == nil {
		return nil, fmt.Errorf("initialize: context is nil")
	}
	if input == nil {
		return nil, fmt.Errorf("initialize: stdin is nil")
	}

	registry := source.NewRegistry()
	registrations := []struct {
		name string
		run  func() error
	}{
		{name: "CPU usage", run: func() error { return linuxcpu.RegisterDefaultUsage(ctx, registry) }},
		{name: "CPU hardware", run: func() error { return linuxcpu.RegisterDefaultHardware(ctx, registry) }},
		{name: "Linux procfs", run: func() error { return linuxproc.RegisterDefault(ctx, registry) }},
		{name: "Linux GPU", run: func() error { return linuxgpu.RegisterDefault(ctx, registry) }},
		{name: "stdin", run: func() error {
			return stdinsource.Register(registry, func(openContext context.Context) (io.Reader, error) {
				if err := openContext.Err(); err != nil {
					return nil, err
				}
				return input, nil
			}, time.Now)
		}},
	}
	for _, registration := range registrations {
		if err := registration.run(); err != nil {
			return nil, fmt.Errorf("initialize %s sources: %w", registration.name, err)
		}
	}

	return &Runner{Registry: registry, Backend: csound.New("")}, nil
}

// Run parses and executes exactly one command. It writes only discovery,
// telemetry, or raw PCM data to output. Diagnostics is passed only to the
// audio backend; all other errors are returned to the process entrypoint.
func (runner *Runner) Run(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	if ctx == nil {
		return fmt.Errorf("execute: context is nil")
	}
	if runner == nil {
		return fmt.Errorf("execute: runner is nil")
	}
	if runner.Registry == nil {
		return fmt.Errorf("execute: source registry is nil")
	}
	if output == nil {
		return fmt.Errorf("execute: stdout is nil")
	}
	if diagnostics == nil {
		return fmt.Errorf("execute: stderr is nil")
	}

	command, err := cli.Parse(args)
	if err != nil {
		return err
	}
	plan, err := cli.BuildPlan(command, runner.Registry)
	if err != nil {
		return err
	}

	switch plan.Mode {
	case cli.ModeList, cli.ModeInspect, cli.ModePrimitive:
		return discovery.Write(output, runner.Registry, plan)
	case cli.ModeTelemetry:
		collector, err := runner.Registry.NewCollector(ctx, command.Source)
		if err != nil {
			return err
		}
		interval := source.DefaultSampleInterval
		if command.Source == stdinsource.Name {
			interval = 0
		}
		if runner.SampleInterval != nil {
			interval = runner.SampleInterval(command.Source)
		}
		return telemetry.Stream(ctx, output, collector, interval)
	case cli.ModeAudioDevice, cli.ModeRawPCM:
		if runner.Backend == nil {
			return fmt.Errorf("execute audio: backend is nil")
		}
		var pcm io.Writer
		if plan.Mode == cli.ModeRawPCM {
			pcm = output
		}
		engine := stashruntime.Engine{
			Registry:       runner.Registry,
			Backend:        runner.Backend,
			SampleInterval: runner.SampleInterval,
			RhythmInterval: runner.RhythmInterval,
			PCM:            pcm,
			Diagnostics:    diagnostics,
			MaxDelay:       runner.MaxDelay,
		}
		return engine.Run(ctx, plan)
	default:
		return fmt.Errorf("execute: unsupported plan mode %q", plan.Mode)
	}
}
