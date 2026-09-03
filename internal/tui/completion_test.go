package tui

import (
	"strings"
	"testing"
)

func labels(items []Suggestion) []string {
	result := make([]string, len(items))
	for index, item := range items {
		result[index] = item.Label
	}
	return result
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestSourceCompletionUsesLiveRegistryMetadata(t *testing.T) {
	items := Complete(testRegistry(t), []string{"cpu."}, 0)
	if !contains(labels(items), "cpu.usage") || contains(labels(items), "gpu.usage") {
		t.Fatalf("source labels = %v", labels(items))
	}
	for _, item := range items {
		if item.Label == "cpu.usage" && (!strings.Contains(item.Help, "natural range") || !strings.Contains(item.Help, "available")) {
			t.Fatalf("source help = %q", item.Help)
		}
	}
}

func TestSynthCompletionChangesContext(t *testing.T) {
	types := Complete(testRegistry(t), []string{"cpu.usage", "-s f"}, 1)
	if !contains(labels(types), "fm") {
		t.Fatalf("synth types = %v", labels(types))
	}
	parameters := Complete(testRegistry(t), []string{"cpu.usage", "-s fm:bass,"}, 1)
	if !contains(labels(parameters), "index=") || !contains(labels(parameters), "modwave=") {
		t.Fatalf("FM parameters = %v", labels(parameters))
	}
	for _, item := range parameters {
		if item.Label == "index=" && !strings.Contains(item.Help, "audio-rate: true") {
			t.Fatalf("index help = %q", item.Help)
		}
	}
}

func TestDeclaredNodesDriveModulationCompletion(t *testing.T) {
	withBody := []string{"cpu.usage", "-s fm:bass", "-s sub:body", "-x reverb:.7,.4,.2", "-m gpu.usage:"}
	items := labels(Complete(testRegistry(t), withBody, 4))
	for _, want := range []string{"syn.bass.index", "syn.body.cutoff", "reverb.mix"} {
		if !contains(items, want) {
			t.Errorf("targets %v do not contain %q", items, want)
		}
	}
	withoutBody := []string{"cpu.usage", "-s fm:bass", "-x reverb:.7,.4,.2", "-m gpu.usage:"}
	items = labels(Complete(testRegistry(t), withoutBody, 3))
	if contains(items, "syn.body.cutoff") {
		t.Fatalf("deleted synth target remains in %v", items)
	}
}

func TestEffectCompletionComesFromEffectRegistry(t *testing.T) {
	items := Complete(testRegistry(t), []string{"cpu.usage", "-x rev"}, 1)
	if !contains(labels(items), "reverb") {
		t.Fatalf("effect labels = %v", labels(items))
	}
}
