package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/source"
)

type neverCollector struct{}

func (neverCollector) Collect(context.Context) (source.Sample, error) {
	return nil, errors.New("not used")
}

func testRegistry(t *testing.T) *source.Registry {
	t.Helper()
	registry := source.NewRegistry()
	minimum, maximum := 0.0, 100.0
	for _, name := range []string{"cpu.usage", "cpu.freq", "cpu.temp", "gpu.usage"} {
		err := registry.RegisterAvailable(source.Info{Name: name, Kind: source.KindScalar, Unit: "%", NaturalMin: &minimum, NaturalMax: &maximum}, func(context.Context) (source.Collector, error) {
			return neverCollector{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func TestArgsAndCommandPreserveOrdinaryCLIClauses(t *testing.T) {
	lines := []string{"cpu.usage", "--range 0..100", "-s fm:bass,index=4", "", "-x reverb:.7,.4,.2"}
	args, err := Args(lines)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"cpu.usage", "--range", "0..100", "-s", "fm:bass,index=4", "-x", "reverb:.7,.4,.2"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("Args() = %#v, want %#v", args, wantArgs)
	}
	wantCommand := "stash cpu.usage \\\n  --range 0..100 \\\n  -s fm:bass,index=4 \\\n  -x reverb:.7,.4,.2"
	if got := Command(lines); got != wantCommand {
		t.Fatalf("Command() = %q, want %q", got, wantCommand)
	}
}

func TestArgsRejectsMoreThanOneClauseValue(t *testing.T) {
	_, err := Args([]string{"cpu.usage", "-s fm:bass extra"})
	if err == nil || !strings.Contains(err.Error(), "each clause") {
		t.Fatalf("Args() error = %v", err)
	}
}

func TestPastedCommandLinesImportsTerminalCommand(t *testing.T) {
	input := "stash cpu.usage \\\n" +
		"  -b 154 \\\n" +
		"  -r rhythm:1/32:x--x-x-x--xx-x-- \\\n" +
		"  -s fm:test,wave=saw,ratio=2,index=2,gain=.22 \\\n" +
		"  -m syn.test.freq=90..420/exp~120ms \\\n" +
		"  -v .9"
	want := []string{
		"cpu.usage",
		"-b 154",
		"-r rhythm:1/32:x--x-x-x--xx-x--",
		"-s fm:test,wave=saw,ratio=2,index=2,gain=.22",
		"-m syn.test.freq=90..420/exp~120ms",
		"-v .9",
	}
	got, err := pastedCommandLines(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pastedCommandLines() = %#v, want %#v", got, want)
	}
	if command := Command(got); command != input {
		t.Fatalf("Command(imported) = %q, want %q", command, input)
	}
}

func TestPastedCommandLinesImportsOneLineCommand(t *testing.T) {
	got, err := pastedCommandLines("stash cpu.usage -b 154 -s fm:test,gain=.22 -v .9")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cpu.usage", "-b 154", "-s fm:test,gain=.22", "-v .9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pastedCommandLines() = %#v, want %#v", got, want)
	}
}

func TestPastedCommandLinesRejectsMalformedTerminalCommand(t *testing.T) {
	for _, input := range []string{
		"stash",
		"stash -s fm:test",
		"stash cpu.usage -s",
		"stash cpu.usage \\",
	} {
		if _, err := pastedCommandLines(input); err == nil {
			t.Errorf("pastedCommandLines(%q) unexpectedly succeeded", input)
		}
	}
}

func TestAnalyzeDistinguishesValidIncompleteAndInvalid(t *testing.T) {
	registry := testRegistry(t)
	tests := []struct {
		name  string
		lines []string
		want  Validity
	}{
		{"valid", []string{"cpu.usage", "-s fm:bass,index=4"}, Valid},
		{"source prefix", []string{"cpu."}, Incomplete},
		{"source textual prefix", []string{"cpu.u"}, Incomplete},
		{"synth prefix", []string{"cpu.usage", "-s fm:bass,index="}, Incomplete},
		{"telemetry is incomplete instrument", []string{"cpu.usage"}, Incomplete},
		{"invalid", []string{"cpu.nope", "-s fm:bass"}, Invalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Analyze(test.lines, registry).State; got != test.want {
				t.Fatalf("state = %v, want %v", got, test.want)
			}
		})
	}
}

func TestInitialDocumentStartsAnAudioInstrument(t *testing.T) {
	analysis := Analyze(initialLines(testRegistry(t)), testRegistry(t))
	if analysis.State != Valid || analysis.Plan.Mode != cli.ModeAudioDevice {
		t.Fatalf("initial analysis = %#v", analysis)
	}
}

func TestDiagnosticCaptureReturnsPlainLastLine(t *testing.T) {
	capture := &diagnosticCapture{}
	_, _ = capture.Write([]byte("first\n\x1b[mCannot open audio device\x1b[m\n"))
	if got, want := capture.lastLine(), "Cannot open audio device"; got != want {
		t.Fatalf("lastLine() = %q, want %q", got, want)
	}
}
