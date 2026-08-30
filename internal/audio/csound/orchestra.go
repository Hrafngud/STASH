package csound

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/sound"
)

const defaultMaxDelay = 10 * time.Second

func orchestra(config audio.Config) (string, time.Duration, error) {
	if err := config.Validate(); err != nil {
		return "", 0, err
	}
	maxDelay, err := resolvedMaxDelay(config)
	if err != nil {
		return "", 0, err
	}

	var text strings.Builder
	text.WriteString(`<CsoundSynthesizer>
<CsOptions>
</CsOptions>
<CsInstruments>
sr = 48000
ksmps = 32
nchnls = 2
0dbfs = 1

gaStashLeft init 0
gaStashRight init 0

opcode StashADSR, k, kkkkk
  kGate, kAttack, kDecay, kSustain, kRelease xin
  kState init 0
  kLevel init 0
  kStart init 0
  kElapsed init 0
  kPreviousGate init 0
  kDelta = ksmps / sr

  if (kGate > 0.5 && kPreviousGate <= 0.5) then
    kState = 1
    kStart = kLevel
    kElapsed = 0
  elseif (kGate <= 0.5 && kPreviousGate > 0.5) then
    kState = 4
    kStart = kLevel
    kElapsed = 0
  endif

  if (kState == 1) then
    if (kAttack <= 0) then
      kLevel = 1
      kState = 2
      kElapsed = 0
    else
      kElapsed = kElapsed + kDelta
      kLevel = kStart + ((1 - kStart) * min(kElapsed / kAttack, 1))
      if (kElapsed >= kAttack) then
        kLevel = 1
        kState = 2
        kElapsed = 0
      endif
    endif
  elseif (kState == 2) then
    if (kDecay <= 0) then
      kLevel = kSustain
      kState = 3
    else
      kElapsed = kElapsed + kDelta
      kLevel = 1 + ((kSustain - 1) * min(kElapsed / kDecay, 1))
      if (kElapsed >= kDecay) then
        kLevel = kSustain
        kState = 3
      endif
    endif
  elseif (kState == 3) then
    kLevel = kSustain
  elseif (kState == 4) then
    if (kRelease <= 0) then
      kLevel = 0
      kState = 0
    else
      kElapsed = kElapsed + kDelta
      kLevel = kStart * (1 - min(kElapsed / kRelease, 1))
      if (kElapsed >= kRelease) then
        kLevel = 0
        kState = 0
      endif
    endif
  endif

  kPreviousGate = kGate
  xout kLevel
endop

instr 1
`)
	for index, voice := range config.Model.Voices {
		writeVoiceInitializers(&text, index, voice)
	}
	for index, effect := range config.Model.Effects {
		writeEffectInitializers(&text, index, effect)
	}
	text.WriteString(`  turnoff
endin

instr 2
  SChannel strget p4
  chnset p5, SChannel
endin

`)

	for index, voice := range config.Model.Voices {
		writeVoiceInstrument(&text, index, voice.Waveform)
	}
	writeOutputInstrument(&text, config.Model.Effects, maxDelay)

	text.WriteString(`</CsInstruments>
<CsScore>
i 1 0 0.01
`)
	for index := range config.Model.Voices {
		fmt.Fprintf(&text, "i %d 0 z\n", voiceInstrument(index))
	}
	text.WriteString(`i 1000 0 z
f 0 z
</CsScore>
</CsoundSynthesizer>
`)
	return text.String(), maxDelay, nil
}

func resolvedMaxDelay(config audio.Config) (time.Duration, error) {
	maximum := config.MaxDelay
	if maximum == 0 {
		maximum = defaultMaxDelay
	}
	for index, effect := range config.Model.Effects {
		if effect.Kind != sound.EffectDelay {
			continue
		}
		if config.MaxDelay > 0 && effect.DelayTime > config.MaxDelay {
			return 0, fmt.Errorf("effect %d delay time %s exceeds configured maximum %s", index, effect.DelayTime, config.MaxDelay)
		}
		if effect.DelayTime > maximum {
			maximum = effect.DelayTime
		}
	}
	return maximum, nil
}

func writeVoiceInitializers(text *strings.Builder, index int, voice sound.Voice) {
	values := []struct {
		name  string
		value float64
	}{
		{"freq", voice.Frequency},
		{"gain", voice.Gain},
		{"pan", voice.Pan},
		{"gate", voice.Gate},
		{"attack", voice.Envelope.Attack.Seconds()},
		{"decay", voice.Envelope.Decay.Seconds()},
		{"sustain", voice.Envelope.Sustain},
		{"release", voice.Envelope.Release.Seconds()},
	}
	for _, item := range values {
		fmt.Fprintf(text, "  chnset %s, %s\n", number(item.value), quote(voiceChannel(index, item.name)))
	}
}

func writeEffectInitializers(text *strings.Builder, index int, effect sound.Effect) {
	switch effect.Kind {
	case sound.EffectLowPass, sound.EffectHighPass:
		fmt.Fprintf(text, "  chnset %s, %s\n", number(effect.Cutoff), quote(effectChannel(index, "cutoff")))
		fmt.Fprintf(text, "  chnset %s, %s\n", number(effect.Q), quote(effectChannel(index, "q")))
	case sound.EffectDelay:
		fmt.Fprintf(text, "  chnset %s, %s\n", number(effect.DelayTime.Seconds()), quote(effectChannel(index, "time")))
		fmt.Fprintf(text, "  chnset %s, %s\n", number(effect.Feedback), quote(effectChannel(index, "feedback")))
		fmt.Fprintf(text, "  chnset %s, %s\n", number(effect.Mix), quote(effectChannel(index, "mix")))
	case sound.EffectDrive:
		fmt.Fprintf(text, "  chnset %s, %s\n", number(effect.Amount), quote(effectChannel(index, "amount")))
	}
}

func writeVoiceInstrument(text *strings.Builder, index int, waveform sound.Waveform) {
	instrument := voiceInstrument(index)
	fmt.Fprintf(text, "instr %d\n", instrument)
	fmt.Fprintf(text, "  kFrequency chnget %s\n", quote(voiceChannel(index, "freq")))
	fmt.Fprintf(text, "  kGain chnget %s\n", quote(voiceChannel(index, "gain")))
	fmt.Fprintf(text, "  kPan chnget %s\n", quote(voiceChannel(index, "pan")))
	fmt.Fprintf(text, "  kGate chnget %s\n", quote(voiceChannel(index, "gate")))
	fmt.Fprintf(text, "  kAttack chnget %s\n", quote(voiceChannel(index, "attack")))
	fmt.Fprintf(text, "  kDecay chnget %s\n", quote(voiceChannel(index, "decay")))
	fmt.Fprintf(text, "  kSustain chnget %s\n", quote(voiceChannel(index, "sustain")))
	fmt.Fprintf(text, "  kRelease chnget %s\n", quote(voiceChannel(index, "release")))
	text.WriteString("  kEnvelope StashADSR kGate, kAttack, kDecay, kSustain, kRelease\n")
	switch waveform {
	case sound.WaveSine:
		text.WriteString("  aSignal poscil 1, kFrequency\n")
	case sound.WaveSquare:
		text.WriteString("  aSignal vco2 1, kFrequency, 2, 0.5\n")
	case sound.WaveSaw:
		text.WriteString("  aSignal vco2 1, kFrequency, 0\n")
	case sound.WaveTri:
		text.WriteString("  aSignal vco2 1, kFrequency, 4, 0.5\n")
	case sound.WaveNoise:
		text.WriteString("  aSignal rand 1\n")
	}
	text.WriteString("  kLeft = sqrt((1 - kPan) * 0.5)\n")
	text.WriteString("  kRight = sqrt((1 + kPan) * 0.5)\n")
	text.WriteString("  gaStashLeft = gaStashLeft + (aSignal * kEnvelope * kGain * kLeft)\n")
	text.WriteString("  gaStashRight = gaStashRight + (aSignal * kEnvelope * kGain * kRight)\n")
	text.WriteString("endin\n\n")
}

func writeOutputInstrument(text *strings.Builder, effects []sound.Effect, maxDelay time.Duration) {
	text.WriteString(`instr 1000
  aLeft = gaStashLeft
  aRight = gaStashRight
  clear gaStashLeft, gaStashRight
`)
	for index, effect := range effects {
		switch effect.Kind {
		case sound.EffectLowPass, sound.EffectHighPass:
			mode := 0
			if effect.Kind == sound.EffectHighPass {
				mode = 1
			}
			fmt.Fprintf(text, "  kCutoff%d chnget %s\n", index, quote(effectChannel(index, "cutoff")))
			fmt.Fprintf(text, "  kQ%d chnget %s\n", index, quote(effectChannel(index, "q")))
			fmt.Fprintf(text, "  kCutoff%d limit kCutoff%d, 1, (sr * 0.499)\n", index, index)
			fmt.Fprintf(text, "  aLeft rbjeq aLeft, kCutoff%d, 0, kQ%d, %d\n", index, index, mode)
			fmt.Fprintf(text, "  aRight rbjeq aRight, kCutoff%d, 0, kQ%d, %d\n", index, index, mode)
		case sound.EffectDelay:
			fmt.Fprintf(text, "  kDelayTime%d chnget %s\n", index, quote(effectChannel(index, "time")))
			fmt.Fprintf(text, "  kFeedback%d chnget %s\n", index, quote(effectChannel(index, "feedback")))
			fmt.Fprintf(text, "  kMix%d chnget %s\n", index, quote(effectChannel(index, "mix")))
			fmt.Fprintf(text, "  kDelayTime%d limit kDelayTime%d, (1 / sr), %s\n", index, index, number(maxDelay.Seconds()))
			fmt.Fprintf(text, "  aDelayBufferLeft%d delayr %s\n", index, number(maxDelay.Seconds()))
			fmt.Fprintf(text, "  aDelayTapLeft%d deltap3 kDelayTime%d\n", index, index)
			fmt.Fprintf(text, "  delayw aLeft + (aDelayTapLeft%d * kFeedback%d)\n", index, index)
			fmt.Fprintf(text, "  aDelayBufferRight%d delayr %s\n", index, number(maxDelay.Seconds()))
			fmt.Fprintf(text, "  aDelayTapRight%d deltap3 kDelayTime%d\n", index, index)
			fmt.Fprintf(text, "  delayw aRight + (aDelayTapRight%d * kFeedback%d)\n", index, index)
			fmt.Fprintf(text, "  aLeft = (aLeft * (1 - kMix%d)) + (aDelayTapLeft%d * kMix%d)\n", index, index, index)
			fmt.Fprintf(text, "  aRight = (aRight * (1 - kMix%d)) + (aDelayTapRight%d * kMix%d)\n", index, index, index)
		case sound.EffectDrive:
			fmt.Fprintf(text, "  kDrive%d chnget %s\n", index, quote(effectChannel(index, "amount")))
			fmt.Fprintf(text, "  aLeft = (aLeft * (1 - kDrive%d)) + (tanh(aLeft * 10) * kDrive%d)\n", index, index)
			fmt.Fprintf(text, "  aRight = (aRight * (1 - kDrive%d)) + (tanh(aRight * 10) * kDrive%d)\n", index, index)
		}
	}
	text.WriteString("  outs aLeft, aRight\nendin\n\n")
}

func voiceInstrument(index int) int {
	return 100 + index
}

func voiceChannel(index int, parameter string) string {
	return fmt.Sprintf("voice.%d.%s", index, parameter)
}

func effectChannel(index int, parameter string) string {
	return fmt.Sprintf("effect.%d.%s", index, parameter)
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'g', 17, 64)
}

func quote(value string) string {
	return strconv.Quote(value)
}
