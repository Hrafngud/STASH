package tui

import (
	"testing"

	"github.com/zalmo/stash/internal/cli"
)

func planFor(t *testing.T, lines []string) cli.Plan {
	t.Helper()
	analysis := Analyze(lines, testRegistry(t))
	if analysis.State != Valid {
		t.Fatalf("Analyze(%v) = state %v, error %v", lines, analysis.State, analysis.Err)
	}
	return analysis.Plan
}

func TestHotUpdatesForSynthAndEffectNumericParameters(t *testing.T) {
	oldPlan := planFor(t, []string{"cpu.usage", "-s fm:bass,index=4", "-x reverb:size=.7,damp=.4,mix=.2"})
	newPlan := planFor(t, []string{"cpu.usage", "-s fm:bass,index=9", "-x reverb:size=.7,damp=.4,mix=.35"})
	updates, hot := hotUpdates(oldPlan, newPlan)
	if !hot || len(updates) != 2 {
		t.Fatalf("hotUpdates() = %v, %t; want two hot updates", updates, hot)
	}
	if updates[0].Target.Name != "index" || updates[0].Target.SynthIndex != 0 || updates[0].Value != 9 {
		t.Errorf("synth update = %#v", updates[0])
	}
	if updates[1].Target.Name != "reverb.mix" || updates[1].Target.EffectIndex != 0 || updates[1].Value != .35 {
		t.Errorf("effect update = %#v", updates[1])
	}
}

func TestStructuralChangesRequireRebuild(t *testing.T) {
	base := planFor(t, []string{"cpu.usage", "-s fm:bass,index=4"})
	tests := [][]string{
		{"cpu.usage", "-s sub:bass,cutoff=400"},
		{"cpu.usage", "-s fm:bass,index=4", "-x chorus:.8,.3,.2"},
		{"cpu.freq", "-s fm:bass,index=4"},
		{"cpu.usage", "-s fm:bass,index=4", "-m syn.bass.freq=40..100"},
	}
	for _, lines := range tests {
		if _, hot := hotUpdates(base, planFor(t, lines)); hot {
			t.Errorf("hotUpdates accepted structural change %v", lines)
		}
	}
}
