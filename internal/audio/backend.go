// Package audio defines the backend-neutral boundary between STASH's control
// runtime and an audio renderer. It deliberately has no source or telemetry
// dependencies.
package audio

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zalmo/stash/internal/sound"
)

const (
	SampleRate = 48_000
	Channels   = 2
)

// OutputKind selects either the system's default audio device or the public
// headerless PCM stream.
type OutputKind uint8

const (
	OutputDevice OutputKind = iota
	OutputRawPCM
)

// Config is the complete immutable setup passed to a backend. PCM is required
// only for OutputRawPCM. Diagnostics may be nil to discard backend messages.
// MaxDelay bounds storage allocated by variable delay effects; zero asks the
// backend to choose a safe value that includes every initial delay time.
type Config struct {
	Model       sound.Model
	Output      OutputKind
	PCM         io.Writer
	Diagnostics io.Writer
	MaxDelay    time.Duration
}

// Validate checks backend-independent configuration.
func (config Config) Validate() error {
	if err := config.Model.Validate(); err != nil {
		return fmt.Errorf("invalid audio model: %w", err)
	}
	switch config.Output {
	case OutputDevice:
		if config.PCM != nil {
			return fmt.Errorf("PCM writer is only valid for raw PCM output")
		}
	case OutputRawPCM:
		if config.PCM == nil {
			return fmt.Errorf("raw PCM output requires a writer")
		}
	default:
		return fmt.Errorf("unknown audio output kind %d", config.Output)
	}
	if config.MaxDelay < 0 {
		return fmt.Errorf("maximum delay must be non-negative")
	}
	return nil
}

// Update is one low-latency control-channel write. VoiceIndex is used only by
// voice targets; effect targets carry their fixed effect index in Target.
type Update struct {
	Target     sound.Target
	VoiceIndex int
	Value      float64
}

// Backend starts one persistent rendering session.
type Backend interface {
	Start(context.Context, Config) (Session, error)
}

// Session accepts control updates without replacing voices or resetting
// oscillator phase. Wait reports renderer termination; Close requests a clean
// stop and waits for it.
type Session interface {
	Update(context.Context, Update) error
	Wait() error
	Close() error
}
