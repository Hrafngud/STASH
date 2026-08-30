package sound_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/unit"
)

func targetEffects() []sound.Effect {
	return []sound.Effect{
		{Kind: sound.EffectLowPass, Cutoff: 1000, Q: .7},
		{Kind: sound.EffectDelay, DelayTime: 100 * time.Millisecond, Feedback: .2, Mix: .3},
		{Kind: sound.EffectHighPass, Cutoff: 80, Q: .8},
		{Kind: sound.EffectDrive, Amount: .1},
		{Kind: sound.EffectDelay, DelayTime: 200 * time.Millisecond, Feedback: .4, Mix: .5},
	}
}

func TestResolveEveryNumericTargetAndMostRecentEffect(t *testing.T) {
	t.Parallel()
	effects := targetEffects()
	tests := []struct {
		name  string
		index int
	}{
		{name: "freq", index: -1},
		{name: "gain", index: -1},
		{name: "pan", index: -1},
		{name: "gate", index: -1},
		{name: "filter.cutoff", index: 2},
		{name: "filter.q", index: 2},
		{name: "delay.time", index: 4},
		{name: "delay.feedback", index: 4},
		{name: "delay.mix", index: 4},
		{name: "drive.amount", index: 3},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := sound.ResolveTarget(effects, test.name)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != test.name || got.EffectIndex != test.index {
				t.Fatalf("ResolveTarget(%q) = %#v", test.name, got)
			}
		})
	}
}

func TestResolveTargetRequiresMatchingEffect(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"filter.cutoff", "delay.mix", "drive.amount"} {
		_, err := sound.ResolveTarget(nil, name)
		if err == nil || !strings.Contains(err.Error(), "requires a declared") {
			t.Errorf("ResolveTarget(%q) error = %v", name, err)
		}
	}
	if _, err := sound.ResolveTarget(nil, "pitch"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown target error = %v", err)
	}
}

func TestTargetRangeValidation(t *testing.T) {
	t.Parallel()
	effects := targetEffects()
	tests := []struct {
		name  string
		value unit.Range
		valid bool
	}{
		{name: "freq", value: unit.Range{Min: 80, Max: 2000}, valid: true},
		{name: "freq", value: unit.Range{Min: 0, Max: 1}},
		{name: "gain", value: unit.Range{Min: 0, Max: 1}, valid: true},
		{name: "gain", value: unit.Range{Min: 0, Max: 2}},
		{name: "pan", value: unit.Range{Min: -1, Max: 1}, valid: true},
		{name: "gate", value: unit.Range{Min: 0, Max: 1}, valid: true},
		{name: "filter.q", value: unit.Range{Min: .1, Max: 10}, valid: true},
		{name: "delay.time", value: unit.Range{Min: .01, Max: 1}, valid: true},
		{name: "delay.feedback", value: unit.Range{Min: 0, Max: .95}, valid: true},
		{name: "delay.feedback", value: unit.Range{Min: 0, Max: 1}},
		{name: "delay.mix", value: unit.Range{Min: 0, Max: 1}, valid: true},
		{name: "drive.amount", value: unit.Range{Min: 0, Max: 1}, valid: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target, err := sound.ResolveTarget(effects, test.name)
			if err != nil {
				t.Fatal(err)
			}
			err = target.ValidateRange(test.value)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatalf("ValidateRange(%#v) unexpectedly succeeded", test.value)
			}
		})
	}
}

func TestTargetSetUpdatesBoundVoiceAndEffect(t *testing.T) {
	t.Parallel()
	model := sound.Model{Voices: []sound.Voice{sound.DefaultVoice()}, Effects: targetEffects()}
	frequency, _ := sound.ResolveTarget(model.Effects, "freq")
	feedback, _ := sound.ResolveTarget(model.Effects, "delay.feedback")
	delayTime, _ := sound.ResolveTarget(model.Effects, "delay.time")
	if err := frequency.Set(&model, 0, 880); err != nil {
		t.Fatal(err)
	}
	if err := feedback.Set(&model, 0, .9); err != nil {
		t.Fatal(err)
	}
	if err := delayTime.Set(&model, 0, .25); err != nil {
		t.Fatal(err)
	}
	if model.Voices[0].Frequency != 880 || model.Effects[4].Feedback != .9 ||
		model.Effects[4].DelayTime != 250*time.Millisecond || model.Effects[1].Feedback != .2 {
		t.Fatalf("updated model = %#v", model)
	}
	if err := frequency.Set(&model, 1, 440); err == nil {
		t.Fatal("out-of-range voice index succeeded")
	}
	if err := feedback.Set(&model, 0, 1); err == nil {
		t.Fatal("out-of-range feedback succeeded")
	}
	stale := sound.Target{Name: "filter.cutoff", EffectIndex: 1}
	if err := stale.Set(&model, 0, 500); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched target error = %v", err)
	}
}

func TestTargetValueReadsVoiceAndEffectState(t *testing.T) {
	t.Parallel()
	model := sound.Model{Voices: []sound.Voice{sound.DefaultVoice()}, Effects: targetEffects()}
	tests := []struct {
		name string
		want float64
	}{
		{name: "freq", want: 440},
		{name: "gain", want: .1},
		{name: "pan", want: 0},
		{name: "gate", want: 1},
		{name: "filter.cutoff", want: 80},
		{name: "filter.q", want: .8},
		{name: "drive.amount", want: .1},
		{name: "delay.time", want: .2},
		{name: "delay.feedback", want: .4},
		{name: "delay.mix", want: .5},
	}
	for _, test := range tests {
		target, err := sound.ResolveTarget(model.Effects, test.name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := target.Value(model, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Errorf("%s value = %v, want %v", test.name, got, test.want)
		}
	}
	frequency, _ := sound.ResolveTarget(model.Effects, "freq")
	if _, err := frequency.Value(model, 1); err == nil {
		t.Fatal("out-of-range voice read succeeded")
	}
}
