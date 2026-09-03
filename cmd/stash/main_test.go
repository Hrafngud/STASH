package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/zalmo/stash/internal/app"
	"github.com/zalmo/stash/internal/source"
)

type emptyCollector struct{}

func (emptyCollector) Collect(context.Context) (source.Sample, error) { return nil, io.EOF }

func TestRunPrefixesErrorsOnStderrOnly(t *testing.T) {
	runner := &app.Runner{Registry: source.NewRegistry()}
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"cpu.foobar"}, strings.NewReader(""), &stdout, &stderr, runner)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got, want := stderr.String(), "stash: unknown source: cpu.foobar\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunTreatsSignalContextCancellationAsCleanShutdown(t *testing.T) {
	registry := source.NewRegistry()
	minimum, maximum := 0.0, 100.0
	if err := registry.RegisterAvailable(source.Info{
		Name: "cpu.usage", Kind: source.KindScalar, Unit: "%",
		NaturalMin: &minimum, NaturalMax: &maximum,
	}, func(context.Context) (source.Collector, error) { return emptyCollector{}, nil }); err != nil {
		t.Fatalf("register source: %v", err)
	}
	runner := &app.Runner{Registry: registry}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if exitCode := run(ctx, []string{"cpu.usage"}, strings.NewReader(""), &stdout, &stderr, runner); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q; want both empty", stdout.String(), stderr.String())
	}
}

func TestRunWritesHelpWithoutInitializingRunner(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			// An unusable injected runner proves help exits before runtime
			// dependency validation or source discovery.
			exitCode := run(context.Background(), []string{flag}, strings.NewReader(""), &stdout, &stderr, &app.Runner{})
			if exitCode != 0 {
				t.Fatalf("exit code = %d, want 0", exitCode)
			}
			if !strings.HasPrefix(stdout.String(), "STASH — Sound Telemetry Auto SHell\n") {
				t.Fatalf("stdout does not start with help heading: %q", stdout.String())
			}
			if !strings.Contains(stdout.String(), "stash -h | --help") {
				t.Fatalf("stdout does not document both help flags: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunRejectsHelpCombinedWithOtherArguments(t *testing.T) {
	runner := &app.Runner{Registry: source.NewRegistry()}
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"--help", "cpu.usage"}, strings.NewReader(""), &stdout, &stderr, runner)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown option "--help"`) {
		t.Fatalf("stderr = %q, want unknown-option diagnostic", stderr.String())
	}
}

func TestRunWithoutArgumentsOpensInteractiveEditor(t *testing.T) {
	runner := &app.Runner{Registry: source.NewRegistry()}
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), nil, strings.NewReader("q"), &stdout, &stderr, runner)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "STASH — live instrument") {
		t.Fatalf("stdout does not contain TUI heading: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
