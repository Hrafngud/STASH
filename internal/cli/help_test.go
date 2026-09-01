package cli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalmo/stash/internal/cli"
)

func TestHelpRequestsAreExactTopLevelForms(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		args []string
		want bool
	}{
		{args: []string{"-h"}, want: true},
		{args: []string{"--help"}, want: true},
		{args: nil, want: false},
		{args: []string{"cpu.usage", "--help"}, want: false},
		{args: []string{"-h", "cpu.usage"}, want: false},
	} {
		if got := cli.IsHelpRequest(test.args); got != test.want {
			t.Errorf("IsHelpRequest(%q) = %t, want %t", test.args, got, test.want)
		}
	}
}

func TestHelpDocumentsEveryPublicOption(t *testing.T) {
	t.Parallel()
	for _, option := range []string{
		"-h", "--help", "-l", "-i", "-p", "-w", "-m", "--range", "-v",
		"-t", "-n", "-r", "-b", "-d", "-a", "--swing", "-f", "-x", "-o",
	} {
		if !strings.Contains(cli.HelpText, option) {
			t.Errorf("HelpText does not document %s", option)
		}
	}
	if !strings.HasSuffix(cli.HelpText, "\n") {
		t.Error("HelpText must end with a newline")
	}
}

func TestWriteHelpPropagatesOutputErrors(t *testing.T) {
	t.Parallel()
	err := cli.WriteHelp(errorWriter{})
	if err == nil || !strings.Contains(err.Error(), "write help") {
		t.Fatalf("WriteHelp() error = %v, want contextual write error", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("broken writer") }
