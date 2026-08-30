package audio

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/zalmo/stash/internal/sound"
)

func TestConfigValidate(t *testing.T) {
	model := sound.Model{Voices: []sound.Voice{sound.DefaultVoice()}}
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{name: "device", config: Config{Model: model, Output: OutputDevice}},
		{name: "raw", config: Config{Model: model, Output: OutputRawPCM, PCM: &bytes.Buffer{}}},
		{name: "raw missing writer", config: Config{Model: model, Output: OutputRawPCM}, wantErr: "requires a writer"},
		{name: "device with PCM writer", config: Config{Model: model, Output: OutputDevice, PCM: &bytes.Buffer{}}, wantErr: "only valid for raw PCM"},
		{name: "unknown output", config: Config{Model: model, Output: OutputKind(99)}, wantErr: "unknown audio output"},
		{name: "negative maximum delay", config: Config{Model: model, MaxDelay: -time.Second}, wantErr: "maximum delay"},
		{name: "invalid model", config: Config{Output: OutputDevice}, wantErr: "invalid audio model"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if test.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}
