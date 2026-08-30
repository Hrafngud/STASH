// Package discovery renders the human-readable source and primitive
// inspection modes. It never renders telemetry samples or audio data.
package discovery

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/source"
)

// Write renders one validated discovery plan. Source listing reads the
// registry at render time so it includes every registered available and
// unavailable source.
func Write(output io.Writer, registry *source.Registry, plan cli.Plan) error {
	if output == nil {
		return fmt.Errorf("write discovery: output is nil")
	}

	var document string
	var err error
	switch plan.Mode {
	case cli.ModeList:
		document, err = listDocument(registry, plan.Command.ListPrefix)
	case cli.ModeInspect:
		if !plan.HasSource {
			err = fmt.Errorf("inspect source: plan has no source metadata")
			break
		}
		document = inspectionDocument(plan.SourceEntry)
	case cli.ModePrimitive:
		document, err = primitiveDocument(plan.Command.Primitive, plan.Primitive)
	default:
		err = fmt.Errorf("write discovery: mode %q is not a discovery mode", plan.Mode)
	}
	if err != nil {
		return err
	}
	return writeDocument(output, document)
}

func listDocument(registry *source.Registry, prefix string) (string, error) {
	if registry == nil {
		return "", fmt.Errorf("list sources: source registry is nil")
	}

	var document strings.Builder
	document.WriteString("NAME\tKIND\tUNIT\tAVAILABILITY\n")
	for _, entry := range registry.List() {
		if !strings.HasPrefix(entry.Info.Name, prefix) {
			continue
		}
		fmt.Fprintf(&document, "%s\t%s\t%s\t%s\n",
			entry.Info.Name,
			entry.Info.Kind,
			entry.Info.Unit,
			availability(entry),
		)
	}
	return document.String(), nil
}

func inspectionDocument(entry source.Entry) string {
	var document strings.Builder
	fmt.Fprintf(&document, "name: %s\n", entry.Info.Name)
	fmt.Fprintf(&document, "kind: %s\n", entry.Info.Kind)
	fmt.Fprintf(&document, "unit: %s\n", entry.Info.Unit)
	if entry.Info.NaturalMin == nil || entry.Info.NaturalMax == nil {
		document.WriteString("natural range: unspecified\n")
	} else {
		fmt.Fprintf(&document, "natural range: %s..%s\n",
			formatNumber(*entry.Info.NaturalMin),
			formatNumber(*entry.Info.NaturalMax),
		)
	}
	fmt.Fprintf(&document, "availability: %s\n", availability(entry))
	return document.String()
}

func primitiveDocument(input string, resolution cli.PrimitiveResolution) (string, error) {
	if resolution.Rhythm != nil {
		if resolution.BPM == nil {
			return "", fmt.Errorf("inspect primitive %q: rhythm has no resolved BPM", input)
		}
		return rhythmDocument(input, *resolution.Rhythm, *resolution.BPM), nil
	}
	if len(resolution.Notes) == 0 {
		return "", fmt.Errorf("inspect primitive %q: note resolution is empty", input)
	}
	return notesDocument(input, resolution.Notes), nil
}

func notesDocument(input string, notes []primitive.Note) string {
	kind := "note"
	switch {
	case strings.HasPrefix(input, "scale:"):
		kind = "scale"
	case strings.HasPrefix(input, "mode:"):
		kind = "mode"
	case strings.Contains(input, ","):
		kind = "notes"
	}

	var document strings.Builder
	fmt.Fprintf(&document, "primitive: %s\n", input)
	fmt.Fprintf(&document, "kind: %s\n", kind)
	fmt.Fprintf(&document, "count: %d\n", len(notes))
	document.WriteString("notes:\n")
	for index, note := range notes {
		fmt.Fprintf(&document, "  %d: %s (%s Hz)\n", index, note, formatNumber(note.Frequency()))
	}
	return document.String()
}

func rhythmDocument(input string, rhythm primitive.Rhythm, bpm float64) string {
	var document strings.Builder
	fmt.Fprintf(&document, "primitive: %s\n", input)
	document.WriteString("kind: rhythm\n")
	fmt.Fprintf(&document, "bpm: %s\n", formatNumber(bpm))
	fmt.Fprintf(&document, "division: %s\n", rhythm.Division)
	fmt.Fprintf(&document, "pattern: %s\n", rhythm.Pattern)
	fmt.Fprintf(&document, "steps: %d\n", rhythm.StepCount())
	document.WriteString("controls: rhythm.gate,rhythm.hit,rhythm.step,rhythm.velocity,rhythm.phase\n")
	return document.String()
}

func availability(entry source.Entry) string {
	if entry.Available {
		return "available"
	}
	reason := strings.Join(strings.Fields(entry.UnavailableReason), " ")
	if reason == "" {
		return "unavailable"
	}
	return "unavailable: " + reason
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func writeDocument(output io.Writer, document string) error {
	written, err := io.WriteString(output, document)
	if err != nil {
		return fmt.Errorf("write discovery: %w", err)
	}
	if written != len(document) {
		return fmt.Errorf("write discovery: %w", io.ErrShortWrite)
	}
	return nil
}
