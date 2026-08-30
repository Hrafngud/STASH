package sound_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/sound"
)

func TestDefaultVoiceAndValidation(t *testing.T) {
	t.Parallel()
	voice := sound.DefaultVoice()
	if voice.Waveform != sound.WaveSine || voice.Frequency != 440 || voice.Gain != .1 ||
		voice.Pan != 0 || voice.Gate != 1 || voice.Envelope != sound.DefaultADSR {
		t.Fatalf("default voice = %#v", voice)
	}
	if err := voice.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestVoiceValidationRejectsEveryInvalidParameter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*sound.Voice)
		want string
	}{
		{name: "wave", edit: func(v *sound.Voice) { v.Waveform = "pulse" }, want: "waveform"},
		{name: "frequency", edit: func(v *sound.Voice) { v.Frequency = 0 }, want: "frequency"},
		{name: "gain", edit: func(v *sound.Voice) { v.Gain = 2 }, want: "gain"},
		{name: "pan", edit: func(v *sound.Voice) { v.Pan = -2 }, want: "pan"},
		{name: "gate", edit: func(v *sound.Voice) { v.Gate = math.NaN() }, want: "gate"},
		{name: "attack", edit: func(v *sound.Voice) { v.Envelope.Attack = -time.Millisecond }, want: "attack"},
		{name: "decay", edit: func(v *sound.Voice) { v.Envelope.Decay = -time.Millisecond }, want: "decay"},
		{name: "sustain", edit: func(v *sound.Voice) { v.Envelope.Sustain = 1.1 }, want: "sustain"},
		{name: "release", edit: func(v *sound.Voice) { v.Envelope.Release = -time.Millisecond }, want: "release"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			voice := sound.DefaultVoice()
			test.edit(&voice)
			err := voice.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestModelValidationChecksVoicesAndEffects(t *testing.T) {
	t.Parallel()
	if err := (sound.Model{}).Validate(); err == nil || !strings.Contains(err.Error(), "at least one voice") {
		t.Fatalf("empty model error = %v", err)
	}
	model := sound.Model{
		Voices:  []sound.Voice{sound.DefaultVoice()},
		Effects: []sound.Effect{{Kind: sound.EffectDrive, Amount: 2}},
	}
	if err := model.Validate(); err == nil || !strings.Contains(err.Error(), "effect 0") {
		t.Fatalf("invalid effect error = %v", err)
	}
}
