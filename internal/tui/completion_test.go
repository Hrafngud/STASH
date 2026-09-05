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
		if item.Label == "index=" {
			if !strings.HasSuffix(item.Value, "index=1") {
				t.Fatalf("index completion = %q", item.Value)
			}
			if !strings.Contains(item.Help, "audio-rate: true") {
				t.Fatalf("index help = %q", item.Help)
			}
		}
	}
}

func TestNumericCompletionsInsertDocumentedDefaults(t *testing.T) {
	options := Complete(testRegistry(t), []string{"cpu.usage", ""}, 1)
	values := map[string]string{}
	for _, item := range options {
		values[item.Label] = item.Value
	}
	for label, want := range map[string]string{
		"-v": "-v 0.1", "-d": "-d 100ms", "-a": "-a 5ms,20ms,0.8,50ms", "--swing": "--swing 50",
	} {
		if got := values[label]; got != want {
			t.Errorf("%s completion = %q, want %q", label, got, want)
		}
	}

	withSynth := Complete(testRegistry(t), []string{"cpu.usage", "-s fm:bass", "-v"}, 2)
	if len(withSynth) != 1 || withSynth[0].Value != "-v 1" {
		t.Fatalf("synth master completion = %#v", withSynth)
	}
}

func TestDurationParameterCompletionsIncludeRequiredUnit(t *testing.T) {
	synthItems := Complete(testRegistry(t), []string{"cpu.usage", "-s granular:g,sample=voice.wav,si"}, 1)
	foundSize := false
	for _, item := range synthItems {
		if item.Label == "size=" {
			foundSize = true
			if item.Value != "-s granular:g,sample=voice.wav,size=0.1s" {
				t.Fatalf("granular size completion = %q", item.Value)
			}
		}
	}
	if !foundSize {
		t.Fatal("granular size completion is missing")
	}

	effectItems := Complete(testRegistry(t), []string{"cpu.usage", "-x delay"}, 1)
	if len(effectItems) != 1 || effectItems[0].Value != "-x delay:time=0.15s,feedback=0.4,mix=0.25" {
		t.Fatalf("delay completion = %#v", effectItems)
	}
	if analysis := Analyze([]string{"cpu.usage", effectItems[0].Value}, testRegistry(t)); analysis.State != Valid {
		t.Fatalf("completed delay is invalid: %v", analysis.Err)
	}

	namedEffectItems := Complete(testRegistry(t), []string{"cpu.usage", "-x comp:a"}, 1)
	if len(namedEffectItems) != 1 || namedEffectItems[0].Value != "-x comp:attack=0.005s" {
		t.Fatalf("compressor attack completion = %#v", namedEffectItems)
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
	for _, item := range items {
		if item.Label == "reverb" && item.Value != "-x reverb:size=0.7,damp=0.4,mix=0.25" {
			t.Fatalf("reverb completion = %q", item.Value)
		}
	}
}
