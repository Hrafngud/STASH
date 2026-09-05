package tui

import (
	"strings"
	"testing"
)

func TestClauseMarksLinkTelemetryAndSynthIdentities(t *testing.T) {
	registry := testRegistry(t)
	lines := []string{
		"cpu.usage",
		"-s fm:mod,mix=0",
		"-s sub:voice",
		"-m freq=80..220",
		"-m gpu.usage:syn.voice.gain=.02...2",
		"-m syn.mod.out:syn.voice.freq.mod=-200..200",
	}

	wants := [][]string{
		{sourceIdentity("cpu.usage")},
		{synthIdentity("mod")},
		{synthIdentity("voice")},
		{synthIdentity("voice")},
		{sourceIdentity("gpu.usage"), synthIdentity("voice")},
		{synthIdentity("mod"), synthIdentity("voice")},
	}
	for index, line := range lines {
		marks := clauseMarks(line, index, lines, registry)
		got := make([]string, len(marks))
		for markIndex, mark := range marks {
			got[markIndex] = mark.identity
		}
		if strings.Join(got, ",") != strings.Join(wants[index], ",") {
			t.Errorf("line %d identities = %v, want %v", index+1, got, wants[index])
		}
		if rendered := stripANSI(highlightClause(line, index, lines, registry)); rendered != line {
			t.Errorf("highlighted line %d changed clause text: %q", index+1, rendered)
		}
	}
}

func TestSemanticColorAssignmentIsStable(t *testing.T) {
	identity := sourceIdentity("cpu.usage")
	first := semanticColorIndex(identity)
	for range 10 {
		if got := semanticColorIndex(identity); got != first {
			t.Fatalf("color index changed from %d to %d", first, got)
		}
	}
	if first < 0 || first >= len(semanticColors) {
		t.Fatalf("color index %d outside palette", first)
	}
}

func TestSemanticColorsResolveDocumentCollisions(t *testing.T) {
	registry := testRegistry(t)
	lines := []string{
		"cpu.usage",
		"-s fm:voice",
		"-m syn.voice.index=1..8",
	}
	// These two identities intentionally collide in the base FNV palette.
	if semanticColorIndex(sourceIdentity("cpu.usage")) != semanticColorIndex(synthIdentity("voice")) {
		t.Fatal("collision fixture no longer collides; choose another deterministic pair")
	}
	primary := semanticColorIndexFor(sourceIdentity("cpu.usage"), lines, registry)
	secondary := semanticColorIndexFor(synthIdentity("voice"), lines, registry)
	if primary == secondary {
		t.Fatalf("visible source identities both use palette index %d", primary)
	}
}

func TestInspectorExplainsPrimaryTelemetryAndAudioRateRoutes(t *testing.T) {
	state := newTestEditor(t)
	state.lines = []string{
		"cpu.usage",
		"-s fm:mod,mix=0",
		"-s sub:voice",
		"-m freq=80..220",
		"-m gpu.usage:syn.voice.gain=.02...2",
		"-m syn.mod.out:syn.voice.freq.mod=-200..200",
	}

	state.active = 3
	primary := stripANSI(state.clauseInsight(60))
	for _, want := range []string{"CONTROL-RATE MAP", "cpu.usage", "primary shorthand", "syn.voice.freq", "does not become"} {
		if !strings.Contains(primary, want) {
			t.Errorf("primary insight %q does not contain %q", primary, want)
		}
	}

	state.active = 4
	explicit := stripANSI(state.clauseInsight(60))
	for _, want := range []string{"gpu.usage", "syn.voice.gain", "CONTROL-RATE MAP"} {
		if !strings.Contains(explicit, want) {
			t.Errorf("explicit insight %q does not contain %q", explicit, want)
		}
	}

	state.active = 5
	audioRate := stripANSI(state.clauseInsight(60))
	for _, want := range []string{"AUDIO-RATE ROUTE", "syn.mod.out", "syn.voice.freq.mod", "Only syn.ID.out"} {
		if !strings.Contains(audioRate, want) {
			t.Errorf("audio-rate insight %q does not contain %q", audioRate, want)
		}
	}
}
