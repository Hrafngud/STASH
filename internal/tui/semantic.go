package tui

import (
	"hash/fnv"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
)

// semanticColors are reserved for identities in the patch. Interface chrome
// deliberately uses the neutral styles in ui.go so color always answers the
// question "where else is this source or synth used?".
var semanticColors = []compat.AdaptiveColor{
	adaptiveColor("#075985", "#7DD3FC"), // blue
	adaptiveColor("#9A3412", "#FDBA74"), // vermilion
	adaptiveColor("#047857", "#6EE7B7"), // green
	adaptiveColor("#9D174D", "#F9A8D4"), // pink
	adaptiveColor("#5B21B6", "#C4B5FD"), // violet
	adaptiveColor("#0F766E", "#5EEAD4"), // teal
	adaptiveColor("#92400E", "#FCD34D"), // amber
	adaptiveColor("#9F1239", "#FDA4AF"), // rose
}

type semanticMark struct {
	start    int
	end      int
	identity string
}

func sourceIdentity(name string) string { return "source:" + name }
func synthIdentity(id string) string    { return "synth:" + id }

func semanticColorIndex(identity string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(identity))
	return int(hash.Sum32() % uint32(len(semanticColors)))
}

func semanticStyle(identity string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(semanticColors[semanticColorIndex(identity)])
}

func semanticStyleFor(identity string, lines []string, registry *source.Registry) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(semanticColors[semanticColorIndexFor(identity, lines, registry)])
}

// semanticColorIndexFor starts from the stable identity hash and resolves
// collisions among identities visible in the current patch. That keeps two
// common sources from looking linked merely because their hashes collided.
func semanticColorIndexFor(identity string, lines []string, registry *source.Registry) int {
	identities := semanticIdentities(lines, registry)
	used := make([]bool, len(semanticColors))
	assigned := make(map[string]int, len(identities))
	for _, candidate := range identities {
		index := semanticColorIndex(candidate)
		freeColor := false
		for _, taken := range used {
			if !taken {
				freeColor = true
				break
			}
		}
		if freeColor {
			for used[index] {
				index = (index + 1) % len(semanticColors)
			}
		}
		assigned[candidate] = index
		used[index] = true
	}
	if index, ok := assigned[identity]; ok {
		return index
	}
	return semanticColorIndex(identity)
}

func semanticIdentities(lines []string, registry *source.Registry) []string {
	seen := map[string]bool{}
	for lineIndex, line := range lines {
		for _, mark := range clauseMarks(line, lineIndex, lines, registry) {
			seen[mark.identity] = true
		}
	}
	identities := make([]string, 0, len(seen))
	for identity := range seen {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	return identities
}

// highlightClause applies identity colors without changing the public clause
// text. Marks are found at string boundaries, so byte slicing also preserves
// any UTF-8 text carried by an otherwise shell-safe parameter value.
func highlightClause(line string, lineIndex int, lines []string, registry *source.Registry) string {
	marks := clauseMarks(line, lineIndex, lines, registry)
	if len(marks) == 0 {
		return line
	}
	sort.SliceStable(marks, func(left, right int) bool { return marks[left].start < marks[right].start })
	var result strings.Builder
	position := 0
	for _, mark := range marks {
		if mark.start < position || mark.start < 0 || mark.end > len(line) || mark.start >= mark.end {
			continue
		}
		result.WriteString(line[position:mark.start])
		result.WriteString(semanticStyleFor(mark.identity, lines, registry).Render(line[mark.start:mark.end]))
		position = mark.end
	}
	result.WriteString(line[position:])
	return result.String()
}

func clauseMarks(line string, lineIndex int, lines []string, registry *source.Registry) []semanticMark {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	offset := strings.Index(line, trimmed)
	if !strings.HasPrefix(trimmed, "-") {
		if registry != nil {
			if _, ok := registry.Lookup(trimmed); ok {
				return []semanticMark{{offset, offset + len(trimmed), sourceIdentity(trimmed)}}
			}
		}
		return nil
	}

	option, value, hasValue := strings.Cut(trimmed, " ")
	if !hasValue {
		return nil
	}
	valueOffset := offset + len(option) + 1
	switch option {
	case "-s":
		head, _, _ := strings.Cut(value, ",")
		_, explicitID, hasID := strings.Cut(head, ":")
		synth, ok := synthAtLine(lines, lineIndex)
		if !ok {
			return nil
		}
		if hasID && explicitID != "" {
			start := valueOffset + strings.Index(head, ":") + 1
			return []semanticMark{{start, start + len(explicitID), synthIdentity(synth.ID)}}
		}
		// Omitted IDs do not have a matching token to color, so the type carries
		// the assigned node identity until the user chooses an explicit ID.
		typeName, _, _ := strings.Cut(head, ":")
		return []semanticMark{{valueOffset, valueOffset + len(typeName), synthIdentity(synth.ID)}}
	case "-m":
		left, _, _ := strings.Cut(value, "=")
		control, target, explicitControl := strings.Cut(left, ":")
		var marks []semanticMark
		if explicitControl {
			if identity, start, end, ok := referenceMark(control, valueOffset, lines, registry, true); ok {
				marks = append(marks, semanticMark{start, end, identity})
			}
			targetOffset := valueOffset + len(control) + 1
			if identity, start, end, ok := targetMark(target, targetOffset, lines); ok {
				marks = append(marks, semanticMark{start, end, identity})
			}
			return marks
		}
		if identity, start, end, ok := targetMark(control, valueOffset, lines); ok {
			return []semanticMark{{start, end, identity}}
		}
	case "--range":
		control, _, explicitControl := strings.Cut(value, "=")
		if explicitControl {
			if identity, start, end, ok := referenceMark(control, valueOffset, lines, registry, true); ok {
				return []semanticMark{{start, end, identity}}
			}
		}
	}
	return nil
}

func referenceMark(reference string, offset int, lines []string, registry *source.Registry, allowSource bool) (string, int, int, bool) {
	if allowSource {
		if registry != nil {
			if _, ok := registry.Lookup(reference); ok {
				return sourceIdentity(reference), offset, offset + len(reference), true
			}
		}
	}
	if strings.HasPrefix(reference, "syn.") && strings.HasSuffix(reference, ".out") {
		id := strings.TrimSuffix(strings.TrimPrefix(reference, "syn."), ".out")
		if hasSynthID(lines, id) {
			start := offset + len("syn.")
			return synthIdentity(id), start, start + len(id), true
		}
	}
	return "", 0, 0, false
}

func targetMark(target string, offset int, lines []string) (string, int, int, bool) {
	synths := partialSynths(lines)
	if len(synths) == 0 {
		return "", 0, 0, false
	}
	if strings.HasPrefix(target, "syn.") {
		rest := strings.TrimPrefix(target, "syn.")
		first, _, hasDot := strings.Cut(rest, ".")
		if hasDot && hasSynthID(lines, first) {
			start := offset + len("syn.")
			return synthIdentity(first), start, start + len(first), true
		}
		// syn.PARAM is shorthand for the most recently declared synth.
		start := offset + len("syn.")
		return synthIdentity(synths[len(synths)-1].ID), start, offset + len(target), true
	}
	for _, legacy := range []string{"freq", "gain", "pan", "gate"} {
		if target == legacy {
			return synthIdentity(synths[len(synths)-1].ID), offset, offset + len(target), true
		}
	}
	return "", 0, 0, false
}

func synthAtLine(lines []string, wanted int) (sound.Synth, bool) {
	type indexedSynth struct {
		line  int
		synth sound.Synth
	}
	var parsed []indexedSynth
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "-s ") {
			continue
		}
		synth, err := sound.ParseSynth(strings.TrimSpace(strings.TrimPrefix(line, "-s ")))
		if err == nil {
			parsed = append(parsed, indexedSynth{index, synth})
		}
	}
	synths := make([]sound.Synth, len(parsed))
	for index := range parsed {
		synths[index] = parsed[index].synth
	}
	if sound.AssignSynthIDs(synths) != nil {
		return sound.Synth{}, false
	}
	for index := range parsed {
		if parsed[index].line == wanted {
			return synths[index], true
		}
	}
	return sound.Synth{}, false
}

func hasSynthID(lines []string, id string) bool {
	for _, synth := range partialSynths(lines) {
		if synth.ID == id {
			return true
		}
	}
	return false
}

func highlightReference(reference string, lines []string, registry *source.Registry) string {
	if registry != nil {
		if _, ok := registry.Lookup(reference); ok {
			return semanticStyleFor(sourceIdentity(reference), lines, registry).Render(reference)
		}
	}
	if strings.HasPrefix(reference, "syn.") {
		rest := strings.TrimPrefix(reference, "syn.")
		id, tail, hasDot := strings.Cut(rest, ".")
		if hasDot && hasSynthID(lines, id) {
			return "syn." + semanticStyleFor(synthIdentity(id), lines, registry).Render(id) + "." + tail
		}
	}
	return reference
}

func highlightTargetReference(reference string, lines []string, registry *source.Registry) string {
	identity, start, end, ok := targetMark(reference, 0, lines)
	if !ok {
		return reference
	}
	return reference[:start] + semanticStyleFor(identity, lines, registry).Render(reference[start:end]) + reference[end:]
}
