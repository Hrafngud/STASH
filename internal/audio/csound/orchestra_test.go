package csound

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/sound"
)

func completeModel() sound.Model {
	voices := make([]sound.Voice, 0, 5)
	for _, waveform := range []sound.Waveform{
		sound.WaveSine, sound.WaveSquare, sound.WaveSaw, sound.WaveTri, sound.WaveNoise,
	} {
		voice := sound.DefaultVoice()
		voice.Waveform = waveform
		voices = append(voices, voice)
	}
	return sound.Model{
		Voices: voices,
		Effects: []sound.Effect{
			{Kind: sound.EffectLowPass, Cutoff: 2_000, Q: 0.7},
			{Kind: sound.EffectDrive, Amount: 0.2},
			{Kind: sound.EffectDelay, DelayTime: 150 * time.Millisecond, Feedback: 0.4, Mix: 0.25},
			{Kind: sound.EffectHighPass, Cutoff: 80, Q: 0.8},
		},
	}
}

func TestOrchestraContainsPersistentVoicesAndOrderedEffects(t *testing.T) {
	document, maximum, err := orchestra(audio.Config{Model: completeModel(), Output: audio.OutputDevice})
	if err != nil {
		t.Fatalf("orchestra() error = %v", err)
	}
	if maximum != defaultMaxDelay {
		t.Fatalf("maximum delay = %s, want %s", maximum, defaultMaxDelay)
	}
	for _, fragment := range []string{
		"sr = 48000", "nchnls = 2", "opcode StashADSR", "aSignal poscil",
		"aSignal vco2 1, kFrequency, 2, 0.5", "aSignal vco2 1, kFrequency, 0",
		"aSignal vco2 1, kFrequency, 4, 0.5", "aSignal rand 1", "i 100 0 z",
		`chnget "voice.0.freq"`, `chnget "voice.0.gate"`, "clear gaStashLeft, gaStashRight",
		"outs aLeft, aRight",
	} {
		if !strings.Contains(document, fragment) {
			t.Errorf("orchestra missing %q", fragment)
		}
	}

	positions := []int{
		strings.Index(document, `chnget "effect.0.cutoff"`),
		strings.Index(document, `chnget "effect.1.amount"`),
		strings.Index(document, `chnget "effect.2.time"`),
		strings.Index(document, `chnget "effect.3.cutoff"`),
	}
	for index, position := range positions {
		if position < 0 {
			t.Fatalf("effect %d was not generated", index)
		}
		if index > 0 && positions[index-1] >= position {
			t.Fatalf("effects not generated in model order: %v", positions)
		}
	}
}

func TestOrchestraDelayMaximum(t *testing.T) {
	model := sound.Model{
		Voices:  []sound.Voice{sound.DefaultVoice()},
		Effects: []sound.Effect{{Kind: sound.EffectDelay, DelayTime: 2 * time.Second, Feedback: 0.2, Mix: 0.3}},
	}
	_, _, err := orchestra(audio.Config{Model: model, Output: audio.OutputDevice, MaxDelay: time.Second})
	if err == nil || !strings.Contains(err.Error(), "exceeds configured maximum") {
		t.Fatalf("orchestra() error = %v, want configured maximum error", err)
	}

	_, maximum, err := orchestra(audio.Config{Model: model, Output: audio.OutputDevice, MaxDelay: 3 * time.Second})
	if err != nil {
		t.Fatalf("orchestra() error = %v", err)
	}
	if maximum != 3*time.Second {
		t.Fatalf("maximum delay = %s, want 3s", maximum)
	}
}

func TestGeneratedOrchestraPassesInstalledCsoundSyntaxCheck(t *testing.T) {
	executable, err := exec.LookPath("csound")
	if err != nil {
		t.Skip("Csound is not installed")
	}
	document, _, err := orchestra(audio.Config{Model: completeModel(), Output: audio.OutputDevice})
	if err != nil {
		t.Fatalf("orchestra() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.csd")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	command := exec.Command(executable, "-n", "-d", "-m0", "--syntax-check-only", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Csound syntax check error = %v\n%s", err, output)
	}
}

func TestInstalledCsoundRendersHeaderlessRawPCM(t *testing.T) {
	executable, err := exec.LookPath("csound")
	if err != nil {
		t.Skip("Csound is not installed")
	}
	writer := &countingWriter{}
	var diagnostics bytes.Buffer
	config := audio.Config{
		Model:       completeModel(),
		Output:      audio.OutputRawPCM,
		PCM:         writer,
		Diagnostics: &diagnostics,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := New(executable).Start(ctx, config)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for writer.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v; diagnostics: %s", err, diagnostics.String())
	}
	count, prefix := writer.Result()
	if count == 0 {
		t.Fatalf("Csound produced no raw PCM; diagnostics: %s", diagnostics.String())
	}
	if count%(audio.Channels*4) != 0 {
		t.Fatalf("raw PCM byte count = %d, want complete stereo float32 frames", count)
	}
	if string(prefix) == "RIFF" || string(prefix) == "FORM" {
		t.Fatalf("raw PCM begins with container header %q", prefix)
	}
}

func TestCommandArgumentsSeparatePCMAndDiagnostics(t *testing.T) {
	raw := commandArguments(audio.OutputRawPCM, "/tmp/stash.csd")
	wantRaw := []string{"-d", "-m0", "-h", "-f", "-o", "stdout", "-L", "stdin", "/tmp/stash.csd"}
	if strings.Join(raw, "\x00") != strings.Join(wantRaw, "\x00") {
		t.Fatalf("raw arguments = %#v, want %#v", raw, wantRaw)
	}
	device := commandArguments(audio.OutputDevice, "/tmp/stash.csd")
	wantDevice := []string{"-d", "-m0", "-odac", "-L", "stdin", "/tmp/stash.csd"}
	if strings.Join(device, "\x00") != strings.Join(wantDevice, "\x00") {
		t.Fatalf("device arguments = %#v, want %#v", device, wantDevice)
	}
}

func TestRawPCMWriterPacesBytesAtPublicSampleRate(t *testing.T) {
	current := time.Unix(100, 0)
	var output bytes.Buffer
	var waits []time.Duration
	writer := &realtimePCMWriter{
		ctx:    context.Background(),
		output: &output,
		now:    func() time.Time { return current },
		wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			current = current.Add(delay)
			return nil
		},
	}
	data := make([]byte, rawPCMBytesPerSecond/10)
	if written, err := writer.Write(data); err != nil || written != len(data) {
		t.Fatalf("Write() = %d, %v; want %d, nil", written, err, len(data))
	}
	if len(waits) != 1 || waits[0] != 100*time.Millisecond {
		t.Fatalf("waits = %v, want [100ms]", waits)
	}
	if output.Len() != len(data) {
		t.Fatalf("output bytes = %d, want %d", output.Len(), len(data))
	}
}

func TestBackendStartsUpdatesChannelsAndStopsWithFakeCsound(t *testing.T) {
	executable := fakeCsound(t, "6.18")
	var pcm bytes.Buffer
	var diagnostics bytes.Buffer
	config := audio.Config{
		Model:       sound.Model{Voices: []sound.Voice{sound.DefaultVoice()}},
		Output:      audio.OutputRawPCM,
		PCM:         &pcm,
		Diagnostics: &diagnostics,
	}
	session, err := New(executable).Start(context.Background(), config)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	target, err := sound.ResolveTarget(nil, "freq")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if err := session.Update(context.Background(), audio.Update{Target: target, VoiceIndex: 0, Value: 880}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if pcm.Len() != 0 {
		t.Fatalf("fake renderer unexpectedly wrote PCM: %q", pcm.String())
	}
	if got := diagnostics.String(); !strings.Contains(got, `i 2 0 0.001 "voice.0.freq" 880`) {
		t.Fatalf("diagnostics = %q, want control-channel event", got)
	}
}

func TestBackendReportsMissingAndIncompatibleCsound(t *testing.T) {
	config := audio.Config{Model: sound.Model{Voices: []sound.Voice{sound.DefaultVoice()}}, Output: audio.OutputDevice}
	_, err := New(filepath.Join(t.TempDir(), "missing-csound")).Start(context.Background(), config)
	if err == nil || !strings.Contains(err.Error(), "csound unavailable") {
		t.Fatalf("missing Start() error = %v", err)
	}

	_, err = New(fakeCsound(t, "5.2")).Start(context.Background(), config)
	if err == nil || !strings.Contains(err.Error(), "version 6 or newer") {
		t.Fatalf("incompatible Start() error = %v", err)
	}
}

func TestChannelForTarget(t *testing.T) {
	tests := []struct {
		name       string
		target     sound.Target
		voiceIndex int
		want       string
	}{
		{name: "frequency", target: sound.Target{Name: "freq", EffectIndex: -1}, voiceIndex: 2, want: "voice.2.freq"},
		{name: "gate", target: sound.Target{Name: "gate", EffectIndex: -1}, voiceIndex: 0, want: "voice.0.gate"},
		{name: "filter", target: sound.Target{Name: "filter.cutoff", EffectIndex: 3}, want: "effect.3.cutoff"},
		{name: "delay", target: sound.Target{Name: "delay.feedback", EffectIndex: 1}, want: "effect.1.feedback"},
		{name: "drive", target: sound.Target{Name: "drive.amount", EffectIndex: 4}, want: "effect.4.amount"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := channelForTarget(test.target, test.voiceIndex)
			if err != nil {
				t.Fatalf("channelForTarget() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("channelForTarget() = %q, want %q", got, test.want)
			}
		})
	}
}

func fakeCsound(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "csound")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "Csound version ` + version + `"
  exit 0
fi
for argument in "$@"; do
  if [ "$argument" = "--syntax-check-only" ]; then
    exit 0
  fi
done
while IFS= read -r line; do
  printf '%s\n' "$line" >&2
  if [ "$line" = "e" ]; then
    exit 0
  fi
done
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

type countingWriter struct {
	mu     sync.Mutex
	count  int
	prefix [4]byte
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.count < len(writer.prefix) {
		copy(writer.prefix[writer.count:], value)
	}
	writer.count += len(value)
	return len(value), nil
}

func (writer *countingWriter) Count() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.count
}

func (writer *countingWriter) Result() (int, []byte) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	prefix := append([]byte(nil), writer.prefix[:]...)
	return writer.count, prefix
}
