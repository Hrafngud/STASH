package csound

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/sound"
)

func TestGeneratedSynthOrchestrasPassInstalledCsoundSyntaxCheck(t *testing.T) {
	executable, err := exec.LookPath("csound")
	if err != nil {
		t.Skip("Csound is not installed")
	}
	declarations := []string{
		"sub:voice,wave=saw", "sub:pulse,wave=square,pulsewidth=.25", "fm:voice,ratio=2", "pm:voice,ratio=3,wave=tri",
		"am:voice,ratio=.5", "ring:voice,modfreq=220", "add:voice,partials=4",
		"wavetable:voice,table=metal", "karplus:voice", "modal:voice,model=bell",
	}
	sample := filepath.Join(t.TempDir(), "sample.wav")
	if err := writeSilentWAV(sample); err != nil {
		t.Fatal(err)
	}
	declarations = append(declarations, "wavetable:file,table="+sample, "granular:voice,sample="+sample)
	for _, declaration := range declarations {
		declaration := declaration
		t.Run(declaration, func(t *testing.T) {
			synth, err := sound.ParseSynth(declaration)
			if err != nil {
				t.Fatal(err)
			}
			synths := []sound.Synth{synth}
			if err := sound.AssignSynthIDs(synths); err != nil {
				t.Fatal(err)
			}
			document, _, err := orchestra(audio.Config{Model: sound.Model{Synths: synths, MasterGain: 1}, Output: audio.OutputDevice})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "synth.csd")
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(executable, "-n", "-d", "-m0", "--syntax-check-only", path)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("Csound syntax check: %v\n%s", err, output)
			}
		})
	}
}

func TestAudioRateRoutePassesInstalledCsoundSyntaxCheck(t *testing.T) {
	executable, err := exec.LookPath("csound")
	if err != nil {
		t.Skip("Csound is not installed")
	}
	mod, _ := sound.ParseSynth("sub:mod,mix=0")
	carrier, _ := sound.ParseSynth("fm:carrier")
	synths := []sound.Synth{mod, carrier}
	if err := sound.AssignSynthIDs(synths); err != nil {
		t.Fatal(err)
	}
	target := sound.Target{Name: "freq", EffectIndex: -1, IsSynth: true, SynthIndex: 1, Mod: true}
	model := sound.Model{Synths: synths, AudioRoutes: []sound.AudioRoute{{SourceIndex: 0, Target: target, OutputMin: -800, OutputMax: 800, Curve: "exp", Smoothing: 10 * time.Millisecond}}, MasterGain: 1, MasterGainSet: true}
	document, _, err := orchestra(audio.Config{Model: model, Output: audio.OutputDevice})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "route.csd")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-n", "-d", "-m0", "--syntax-check-only", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Csound syntax check: %v\n%s", err, output)
	}
}

func TestInstalledCsoundRendersModularGraph(t *testing.T) {
	executable, err := exec.LookPath("csound")
	if err != nil {
		t.Skip("Csound is not installed")
	}
	mod, _ := sound.ParseSynth("fm:mod,mix=0,ratio=.25,index=3")
	voice, _ := sound.ParseSynth("sub:voice,wave=saw")
	synths := []sound.Synth{mod, voice}
	if err := sound.AssignSynthIDs(synths); err != nil {
		t.Fatal(err)
	}
	model := sound.Model{Synths: synths, AudioRoutes: []sound.AudioRoute{{SourceIndex: 0, Target: sound.Target{Name: "freq", EffectIndex: -1, IsSynth: true, SynthIndex: 1, Mod: true}, OutputMin: -50, OutputMax: 50, Curve: "linear"}}, MasterGain: 1, MasterGainSet: true}
	writer := &countingWriter{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := New(executable).Start(ctx, audio.Config{Model: model, Output: audio.OutputRawPCM, PCM: writer})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for writer.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if writer.Count() == 0 {
		t.Fatal("modular graph produced no PCM")
	}
}

func writeSilentWAV(path string) error {
	const samples = 480
	var data bytes.Buffer
	data.WriteString("RIFF")
	_ = binary.Write(&data, binary.LittleEndian, uint32(36+samples*2))
	data.WriteString("WAVEfmt ")
	_ = binary.Write(&data, binary.LittleEndian, uint32(16))
	_ = binary.Write(&data, binary.LittleEndian, uint16(1))
	_ = binary.Write(&data, binary.LittleEndian, uint16(1))
	_ = binary.Write(&data, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&data, binary.LittleEndian, uint32(96000))
	_ = binary.Write(&data, binary.LittleEndian, uint16(2))
	_ = binary.Write(&data, binary.LittleEndian, uint16(16))
	data.WriteString("data")
	_ = binary.Write(&data, binary.LittleEndian, uint32(samples*2))
	data.Write(make([]byte, samples*2))
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write WAV: %w", err)
	}
	return nil
}
