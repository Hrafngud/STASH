// Package sound defines STASH's backend-independent signal and effect model.
// It contains no Csound syntax or telemetry acquisition logic.
package sound

import (
	"fmt"
	"math"
	"time"
)

// Waveform identifies one public oscillator waveform.
type Waveform string

const (
	WaveSine   Waveform = "sine"
	WaveSquare Waveform = "square"
	WaveSaw    Waveform = "saw"
	WaveTri    Waveform = "tri"
	WaveNoise  Waveform = "noise"
)

// ADSR describes a voice's amplitude envelope.
type ADSR struct {
	Attack  time.Duration
	Decay   time.Duration
	Sustain float64
	Release time.Duration
}

// Voice is one persistent signal voice. Runtime control updates mutate these
// values without replacing the voice or resetting oscillator phase.
type Voice struct {
	Waveform  Waveform
	Frequency float64
	Gain      float64
	Pan       float64
	Gate      float64
	Envelope  ADSR
}

var DefaultADSR = ADSR{
	Attack:  5 * time.Millisecond,
	Decay:   20 * time.Millisecond,
	Sustain: 0.8,
	Release: 50 * time.Millisecond,
}

// DefaultVoice returns the documented initial signal state. Gate is open for
// continuous sonification; event-driven runtime code may close it before use.
func DefaultVoice() Voice {
	return Voice{
		Waveform:  WaveSine,
		Frequency: 440,
		Gain:      0.1,
		Pan:       0,
		Gate:      1,
		Envelope:  DefaultADSR,
	}
}

// Validate checks all persistent voice parameters.
func (voice Voice) Validate() error {
	switch voice.Waveform {
	case WaveSine, WaveSquare, WaveSaw, WaveTri, WaveNoise:
	default:
		return fmt.Errorf("unknown waveform %q", voice.Waveform)
	}
	if err := validateGreaterThanZero("frequency", voice.Frequency); err != nil {
		return err
	}
	if err := validateRange("gain", voice.Gain, 0, 1); err != nil {
		return err
	}
	if err := validateRange("pan", voice.Pan, -1, 1); err != nil {
		return err
	}
	if err := validateRange("gate", voice.Gate, 0, 1); err != nil {
		return err
	}
	if voice.Envelope.Attack < 0 {
		return fmt.Errorf("invalid ADSR attack: must be non-negative")
	}
	if voice.Envelope.Decay < 0 {
		return fmt.Errorf("invalid ADSR decay: must be non-negative")
	}
	if err := validateRange("ADSR sustain", voice.Envelope.Sustain, 0, 1); err != nil {
		return err
	}
	if voice.Envelope.Release < 0 {
		return fmt.Errorf("invalid ADSR release: must be non-negative")
	}
	return nil
}

func validateFinite(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("invalid %s: must be finite", name)
	}
	return nil
}

func validateGreaterThanZero(name string, value float64) error {
	if err := validateFinite(name, value); err != nil {
		return err
	}
	if value <= 0 {
		return fmt.Errorf("invalid %s: must be greater than zero", name)
	}
	return nil
}

func validateRange(name string, value, minimum, maximum float64) error {
	if err := validateFinite(name, value); err != nil {
		return err
	}
	if value < minimum || value > maximum {
		return fmt.Errorf("invalid %s: must be between %v and %v", name, minimum, maximum)
	}
	return nil
}
