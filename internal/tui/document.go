// Package tui provides STASH's no-argument live instrument editor. It edits
// ordinary CLI clauses and deliberately delegates parsing and planning to the
// same packages used by argument-bearing invocations.
package tui

import (
	"fmt"
	"strings"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/source"
)

// Validity distinguishes a complete command, a useful prefix, and malformed
// input. Incomplete input is intentionally not treated as an error in the UI.
type Validity uint8

const (
	Incomplete Validity = iota
	Valid
	Invalid
)

// Analysis is the current document's compiled representation.
type Analysis struct {
	State Validity
	Args  []string
	Plan  cli.Plan
	Err   error
}

// Args compiles one clause per line to the existing public argv shape. Empty
// lines are editing placeholders and are omitted.
func Args(lines []string) ([]string, error) {
	var args []string
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if strings.HasPrefix(fields[0], "-") {
			if len(fields) == 1 {
				return nil, fmt.Errorf("line %d: option %s needs a value", index+1, fields[0])
			}
			if len(fields) != 2 {
				return nil, fmt.Errorf("line %d: each clause must contain one option and one value", index+1)
			}
			args = append(args, fields...)
			continue
		}
		if len(fields) != 1 {
			return nil, fmt.Errorf("line %d: source must be one token", index+1)
		}
		args = append(args, fields[0])
	}
	return args, nil
}

// Analyze compiles and validates a document against the live source registry.
func Analyze(lines []string, registry *source.Registry) Analysis {
	args, err := Args(lines)
	if err != nil {
		return Analysis{State: classifyError(lines, registry, err), Args: args, Err: err}
	}
	if len(args) == 0 {
		return Analysis{State: Incomplete, Args: args}
	}
	if strings.HasPrefix(args[0], "-") {
		err = fmt.Errorf("source must be the first non-empty clause")
		return Analysis{State: Invalid, Args: args, Err: err}
	}
	command, err := cli.Parse(args)
	if err != nil {
		return Analysis{State: classifyError(lines, registry, err), Args: args, Err: err}
	}
	plan, err := cli.BuildPlan(command, registry)
	if err != nil {
		return Analysis{State: classifyError(lines, registry, err), Args: args, Err: err}
	}
	if plan.Mode != cli.ModeAudioDevice {
		return Analysis{State: Incomplete, Args: args, Plan: plan, Err: fmt.Errorf("add a sound clause to start audio")}
	}
	return Analysis{State: Valid, Args: args, Plan: plan}
}

func classifyError(lines []string, registry *source.Registry, err error) Validity {
	message := err.Error()
	for _, marker := range []string{"requires a value", "needs a value", "must not be empty", "expected [", "requires a declared", "requires -r", "requires table", "requires model", "requires sample"} {
		if strings.Contains(message, marker) {
			return Incomplete
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, ",") || strings.HasSuffix(trimmed, "=") {
			return Incomplete
		}
	}
	for index := range lines {
		current := strings.TrimSpace(lines[index])
		for _, suggestion := range Complete(registry, lines, index) {
			if suggestion.Value != current {
				return Incomplete
			}
		}
	}
	return Invalid
}

// Command renders the document as a reusable ordinary shell command. STASH's
// canonical tokens are shell-safe by contract, so no extra quoting language is
// introduced here.
func Command(lines []string) string {
	clauses := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			clauses = append(clauses, trimmed)
		}
	}
	if len(clauses) == 0 {
		return "stash"
	}
	if len(clauses) == 1 {
		return "stash " + clauses[0]
	}
	return "stash " + strings.Join(clauses, " \\\n  ")
}

// pastedCommandLines turns the shell-safe command emitted by Command back into
// the editor's one-clause-per-line representation. It intentionally supports
// STASH's command language rather than a general shell quoting language.
func pastedCommandLines(input string) ([]string, error) {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = strings.ReplaceAll(input, "\\\n", " ")
	fields := strings.Fields(input)
	if len(fields) == 0 || fields[0] != "stash" {
		return nil, fmt.Errorf("expected a command beginning with stash")
	}
	fields = fields[1:]
	if len(fields) == 0 {
		return nil, fmt.Errorf("stash command needs a source")
	}
	for _, field := range fields {
		if field == "\\" {
			return nil, fmt.Errorf("dangling line continuation")
		}
	}
	if strings.HasPrefix(fields[0], "-") && fields[0] != "-" {
		return nil, fmt.Errorf("stash command must begin with a source")
	}

	lines := []string{fields[0]}
	for index := 1; index < len(fields); index += 2 {
		option := fields[index]
		if !strings.HasPrefix(option, "-") {
			return nil, fmt.Errorf("unexpected positional argument %q", option)
		}
		if index+1 >= len(fields) {
			return nil, fmt.Errorf("option %s needs a value", option)
		}
		lines = append(lines, option+" "+fields[index+1])
	}
	return lines, nil
}

func isCommandPaste(input string) bool {
	fields := strings.Fields(input)
	return len(fields) > 0 && fields[0] == "stash"
}

func initialLines(registry *source.Registry) []string {
	sourceName := ""
	if entry, ok := registry.Lookup("cpu.usage"); ok && entry.Available {
		sourceName = entry.Info.Name
	} else {
		for _, entry := range registry.List() {
			if entry.Available && entry.Info.Name != "-" {
				sourceName = entry.Info.Name
				break
			}
		}
	}
	if sourceName == "" {
		return []string{""}
	}
	return []string{sourceName, "-s fm:bass,ratio=2,index=4"}
}
