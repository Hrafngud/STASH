package sound_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/sound"
)

func TestParseFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  string
		kind   sound.EffectKind
		cutoff float64
		q      float64
	}{
		{input: "lp:2k", kind: sound.EffectLowPass, cutoff: 2000, q: sound.DefaultFilterQ},
		{input: "lp:2k,.7", kind: sound.EffectLowPass, cutoff: 2000, q: 0.7},
		{input: "hp:80", kind: sound.EffectHighPass, cutoff: 80, q: sound.DefaultFilterQ},
		{input: "hp:.5,12", kind: sound.EffectHighPass, cutoff: 0.5, q: 12},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := sound.ParseFilter(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != test.kind || got.Cutoff != test.cutoff || got.Q != test.q {
				t.Fatalf("ParseFilter(%q) = %#v", test.input, got)
			}
		})
	}
}

func TestParseEffects(t *testing.T) {
	t.Parallel()
	delay, err := sound.ParseEffect("delay:150ms,.4,.25")
	if err != nil {
		t.Fatal(err)
	}
	if delay.Kind != sound.EffectDelay || delay.DelayTime != 150*time.Millisecond ||
		delay.Feedback != 0.4 || delay.Mix != 0.25 {
		t.Fatalf("delay = %#v", delay)
	}
	drive, err := sound.ParseEffect("drive:.5")
	if err != nil {
		t.Fatal(err)
	}
	if drive.Kind != sound.EffectDrive || drive.Amount != 0.5 {
		t.Fatalf("drive = %#v", drive)
	}
}

func TestEffectParsersRejectMalformedOrOutOfRangeValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		filter bool
		input  string
		want   string
	}{
		{filter: true, input: "lp", want: "invalid filter"},
		{filter: true, input: "bp:1k", want: "unknown filter"},
		{filter: true, input: "lp:0", want: "greater than zero"},
		{filter: true, input: "hp:80,0", want: "greater than zero"},
		{filter: true, input: "lp:1k,.7,2", want: "invalid filter"},
		{input: "chorus:.2", want: "unknown effect"},
		{input: "delay:0ms,.2,.3", want: "greater than zero"},
		{input: "delay:1ms,.951,.3", want: "between 0 and 0.95"},
		{input: "delay:1ms,.2,1.1", want: "between 0 and 1"},
		{input: "delay:1ms,.2", want: "expected delay"},
		{input: "drive:-.1", want: "between 0 and 1"},
		{input: "drive:.2,.3", want: "expected drive"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			var err error
			if test.filter {
				_, err = sound.ParseFilter(test.input)
			} else {
				_, err = sound.ParseEffect(test.input)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parse %q error = %v, want substring %q", test.input, err, test.want)
			}
		})
	}
}
