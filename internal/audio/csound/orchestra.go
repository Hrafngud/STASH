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
`)
	for index := range config.Model.Synths {
		fmt.Fprintf(&text, "gaStashSynth%d init 0\n", index)
	}
	text.WriteString(`

giStashSine ftgen 0, 0, 16384, 10, 1
giStashSquare ftgen 0, 0, 16384, 7, 1, 8192, 1, 0, -1, 8192, -1
giStashSaw ftgen 0, 0, 16384, 7, -1, 16384, 1
giStashTri ftgen 0, 0, 16384, 7, -1, 8192, 1, 8192, -1
giStashHann ftgen 0, 0, 16384, 20, 2, 1

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
	for index, synth := range config.Model.Synths {
		writeSynthInitializers(&text, index, synth)
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
	for index, synth := range config.Model.Synths {
		writeSynthInstrument(&text, config.Model, index, synth)
	}
	writeOutputInstrument(&text, config.Model.Effects, maxDelay, config.Model.MasterGain, config.Model.MasterGainSet)

	text.WriteString(`</CsInstruments>
<CsScore>
i 1 0 0.01
`)
	for index := range config.Model.Voices {
		fmt.Fprintf(&text, "i %d 0 z\n", voiceInstrument(index))
	}
	for index := range config.Model.Synths {
		fmt.Fprintf(&text, "i %d 0 z\n", synthInstrument(index))
	}
	text.WriteString(`i 1000 0 z
f 0 z
</CsScore>
</CsoundSynthesizer>
`)
	return text.String(), maxDelay, nil
}

func writeSynthInitializers(text *strings.Builder, index int, synth sound.Synth) {
	for _, name := range sound.SortedSynthParameterNames(synth) {
		value := synth.Parameters[name] + synth.Modulations[name]
		fmt.Fprintf(text, "  chnset %s, %s\n", number(value), quote(synthChannel(index, name)))
	}
	values := []struct {
		name  string
		value float64
	}{
		{"attack", synth.Envelope.Attack.Seconds()}, {"decay", synth.Envelope.Decay.Seconds()},
		{"sustain", synth.Envelope.Sustain}, {"release", synth.Envelope.Release.Seconds()},
	}
	for _, item := range values {
		fmt.Fprintf(text, "  chnset %s, %s\n", number(item.value), quote(synthChannel(index, item.name)))
	}
}

func writeSynthInstrument(text *strings.Builder, model sound.Model, index int, synth sound.Synth) {
	fmt.Fprintf(text, "instr %d\n", synthInstrument(index))
	for _, name := range sound.SortedSynthParameterNames(synth) {
		fmt.Fprintf(text, "  k%s chnget %s\n", csName(name), quote(synthChannel(index, name)))
	}
	for _, name := range []string{"attack", "decay", "sustain", "release"} {
		fmt.Fprintf(text, "  k%s chnget %s\n", csName(name), quote(synthChannel(index, name)))
	}
	text.WriteString("  kEnvelope StashADSR kGate, kAttack, kDecay, kSustain, kRelease\n")
	for _, name := range sound.SortedSynthParameterNames(synth) {
		if parameter, ok := synth.Spec().Parameters[name]; ok && parameter.AudioRate {
			writeAudioParameter(text, model, index, name)
		}
	}

	switch synth.Type {
	case sound.SynthSub:
		if synth.Config["wave"] == "square" {
			text.WriteString("  aSafeFreq limit aFreq, 1, (sr * .49)\n  aPulsePhase phasor aSafeFreq\n  aRaw = tanh((cos(aPulsePhase * 6.283185307179586) - cos(aPulsewidth * 3.141592653589793)) * 100)\n")
		} else {
			writeOscillator(text, "aRaw", synth.Config["wave"], rateName(synth, "freq"), "kPulsewidth")
		}
		text.WriteString("  kSafeCutoff limit kCutoff, 1, (sr * .49)\n")
		if synth.Config["filter"] == "hp" {
			text.WriteString("  aLow moogladder aRaw, kSafeCutoff, min(kQ / 10, .99)\n  aSignal = aRaw - aLow\n")
		} else {
			text.WriteString("  aSignal moogladder aRaw, kSafeCutoff, min(kQ / 10, .99)\n")
		}
	case sound.SynthFM:
		writeModulator(text, synth)
		text.WriteString("  aCarrierFreq = aFreq + (aMod * aIndex * aModFreq)\n  aCarrierFreq limit aCarrierFreq, 1, (sr * .49)\n")
		writeOscillator(text, "aSignal", synth.Config["wave"], "aCarrierFreq", ".5")
	case sound.SynthPM:
		writeModulator(text, synth)
		text.WriteString("  aSafeFreq limit aFreq, 1, (sr * .49)\n  aCarrierPhase phasor aSafeFreq\n  aPhase = frac(aCarrierPhase + (aMod * aIndex / 6.283185307179586))\n")
		if synth.Config["wave"] == "noise" {
			text.WriteString("  aSignal rand 1\n")
		} else {
			fmt.Fprintf(text, "  aSignal tablei (aPhase * 16383), %s\n", waveformTable(synth.Config["wave"]))
		}
	case sound.SynthAM:
		writeModulator(text, synth)
		writeOscillator(text, "aCarrier", synth.Config["wave"], "aFreq", ".5")
		text.WriteString("  aSignal = aCarrier * ((1 - aDepth) + (aDepth * ((aMod + 1) * .5)))\n")
	case sound.SynthRing:
		writeModulator(text, synth)
		writeOscillator(text, "aCarrier", synth.Config["wave"], "aFreq", ".5")
		text.WriteString("  aSignal = aCarrier * aMod\n")
	case sound.SynthAdd:
		count, _ := strconv.Atoi(synth.Config["partials"])
		if count < 1 {
			count = 8
		}
		text.WriteString("  aSignal = 0\n")
		for partial := 0; partial < count; partial++ {
			fmt.Fprintf(text, "  aPartialFreq%d = (aFreq * kPartial%dRatio) + kPartial%dDetune\n  aPartialFreq%d limit aPartialFreq%d, 1, (sr * .49)\n", partial, partial, partial, partial, partial)
			writeOscillator(text, fmt.Sprintf("aPartialRaw%d", partial), synth.Config["wave"], fmt.Sprintf("aPartialFreq%d", partial), ".5")
			fmt.Fprintf(text, "  aPartial%d = aPartialRaw%d * kPartial%dGain\n  aSignal = aSignal + aPartial%d\n", partial, partial, partial, partial)
		}
		fmt.Fprintf(text, "  aSignal = aSignal / %s\n", number(harmonicSum(count)))
	case sound.SynthWavetable:
		writeWavetable(text, synth)
	case sound.SynthKarplus:
		text.WriteString("  kHit trigger kGate, .5, 0\n  aExcitation rand (kHit * kExcite * (0.2 + (kBrightness * .8)))\n  kStringFreq downsamp aFreq\n  kDampCutoff = 200 + (kDamping * 14000)\n  aSignal wguide1 aExcitation, max(kStringFreq, 1), kDampCutoff, min(kFeedback, .999)\n")
	case sound.SynthModal:
		writeModal(text, synth)
	case sound.SynthGranular:
		writeGranular(text, index, synth)
	}
	text.WriteString("  aNode = aSignal * kEnvelope * aGain\n")
	fmt.Fprintf(text, "  gaStashSynth%d = aNode\n", index)
	if synth.Type == sound.SynthGranular {
		text.WriteString("  kSpreadValue downsamp aSpread\n  kSpreadPan randomi -kSpreadValue, kSpreadValue, max(kGrainDensity, 1)\n  kNodePan limit (kPan + kSpreadPan), -1, 1\n")
	} else {
		text.WriteString("  kNodePan = kPan\n")
	}
	text.WriteString("  kLeft = sqrt((1 - kNodePan) * .5)\n  kRight = sqrt((1 + kNodePan) * .5)\n  gaStashLeft = gaStashLeft + (aNode * kMix * kLeft)\n  gaStashRight = gaStashRight + (aNode * kMix * kRight)\nendin\n\n")
}

func writeAudioParameter(text *strings.Builder, model sound.Model, targetIndex int, name string) {
	variable := "a" + csName(name)
	fmt.Fprintf(text, "  %s interp k%s\n", variable, csName(name))
	for routeIndex, route := range model.AudioRoutes {
		if route.Target.SynthIndex != targetIndex || route.Target.Name != name {
			continue
		}
		normalized := fmt.Sprintf("((gaStashSynth%d + 1) * .5)", route.SourceIndex)
		switch route.Curve {
		case "exp":
			normalized = "((exp(" + normalized + ") - 1) / (exp(1) - 1))"
		case "log":
			normalized = "log(1 + ((exp(1) - 1) * " + normalized + "))"
		}
		mapped := fmt.Sprintf("(%s + (%s * %s))", number(route.OutputMin), normalized, number(route.OutputMax-route.OutputMin))
		if route.Smoothing > 0 {
			fmt.Fprintf(text, "  aRoute%d = %s\n  aRouteSmoothed%d tone aRoute%d, %s\n", routeIndex, mapped, routeIndex, routeIndex, number(1/(2*3.141592653589793*route.Smoothing.Seconds())))
			mapped = fmt.Sprintf("aRouteSmoothed%d", routeIndex)
		}
		if route.Target.Mod {
			fmt.Fprintf(text, "  %s = %s + %s\n", variable, variable, mapped)
		} else {
			fmt.Fprintf(text, "  %s = %s\n", variable, mapped)
		}
	}
}

func writeModulator(text *strings.Builder, synth sound.Synth) {
	if synth.Config["_modfreq"] != "true" {
		text.WriteString("  aModFreq = aFreq * aRatio\n")
	}
	writeOscillator(text, "aModRaw", synth.Config["modwave"], "aModFreq", ".5")
	if synth.Type == sound.SynthFM || synth.Type == sound.SynthPM {
		text.WriteString("  aModPrevious delay1 aModRaw\n  aMod = tanh(aModRaw + (aModPrevious * aFeedback))\n")
	} else {
		text.WriteString("  aMod = aModRaw\n")
	}
}

func writeOscillator(text *strings.Builder, output, wave, frequency, pulsewidth string) {
	switch wave {
	case "square":
		fmt.Fprintf(text, "  %s oscili 1, %s, giStashSquare\n", output, frequency)
	case "saw":
		fmt.Fprintf(text, "  %s oscili 1, %s, giStashSaw\n", output, frequency)
	case "tri":
		fmt.Fprintf(text, "  %s oscili 1, %s, giStashTri\n", output, frequency)
	case "noise":
		fmt.Fprintf(text, "  %s rand 1\n", output)
	default:
		fmt.Fprintf(text, "  %s oscili 1, %s, giStashSine\n", output, frequency)
	}
	_ = pulsewidth
}

func waveformTable(wave string) string {
	switch wave {
	case "square":
		return "giStashSquare"
	case "saw":
		return "giStashSaw"
	case "tri":
		return "giStashTri"
	default:
		return "giStashSine"
	}
}

func writeWavetable(text *strings.Builder, synth sound.Synth) {
	tableA, tableB := "giStashSine", "giStashSaw"
	switch synth.Config["table"] {
	case "metal":
		tableA, tableB = "giStashSquare", "giStashSine"
	case "digital":
		tableA, tableB = "giStashSaw", "giStashSquare"
	case "smooth":
		tableA, tableB = "giStashSine", "giStashTri"
	default:
		fmt.Fprintf(text, "  iUserTable ftgen 0, 0, 0, 1, %s, 0, 0, 1\n", quote(synth.Config["table"]))
		tableA, tableB = "iUserTable", "iUserTable"
	}
	text.WriteString("  aSafeScan limit aScan, .000001, (sr * .49)\n  aScanPhase phasor aSafeScan\n  aTablePosition = frac(aPosition + aScanPhase)\n  aSafeFreq limit aFreq, 1, (sr * .49)\n  aPhase phasor aSafeFreq\n")
	fmt.Fprintf(text, "  aFrameA tablei (aPhase * 16383), %s\n  aFrameB tablei (aPhase * 16383), %s\n  aSignal = (aFrameA * (1 - aTablePosition)) + (aFrameB * aTablePosition)\n", tableA, tableB)
}

func writeModal(text *strings.Builder, synth sound.Synth) {
	ratios := []float64{1, 2.01, 3.9, 5.4}
	switch synth.Config["model"] {
	case "wood":
		ratios = []float64{1, 2, 3, 4.2}
	case "glass":
		ratios = []float64{1, 2.32, 4.25, 6.63}
	case "bell":
		ratios = []float64{1, 2.71, 5.13, 8.4}
	case "plate":
		ratios = []float64{1, 1.59, 2.14, 2.92}
	}
	text.WriteString("  kHit trigger kGate, .5, 0\n  aImpulse mpulse (kHit * kExcite), 0\n  aSignal = 0\n")
	for index, ratio := range ratios {
		fmt.Fprintf(text, "  aMode%d reson aImpulse, max(kFreq * (%s + (kInharmonicity * %d * .1)), 1), max(1 / max(kDecay, .001), 1)\n  aSignal = aSignal + (aMode%d * %s)\n", index, number(ratio), index, index, number(1/float64(index+1)))
	}
	text.WriteString("  aSignal = aSignal * (.2 + (kBrightness * .8))\n")
}

func writeGranular(text *strings.Builder, index int, synth sound.Synth) {
	fmt.Fprintf(text, "  iSample%d ftgen 0, 0, 0, 1, %s, 0, 0, 1\n", index, quote(synth.Config["sample"]))
	text.WriteString("  kGrainFreq downsamp aFreq\n  kGrainPitch downsamp aPitch\n  kGrainPosition downsamp aPosition\n  kGrainJitter downsamp aJitter\n  kGrainSize downsamp aSize\n  kGrainDensity downsamp aDensity\n  kPositionNoise randh kGrainJitter, max(kGrainDensity, .001)\n")
	fmt.Fprintf(text, "  aSignal grain3 max(kGrainPitch * kGrainFreq, 1), limit(kGrainPosition + kPositionNoise, 0, 1), 0, 0, max(kGrainSize, .001), max(kGrainDensity, .001), 100, iSample%d, giStashHann, 0, 0, %d, 0\n", index, index+1)
}

func rateName(synth sound.Synth, name string) string {
	if synth.Spec().Parameters[name].AudioRate {
		return "a" + csName(name)
	}
	return "k" + csName(name)
}
func csName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '.' || r == '-' })
	for index := range parts {
		if parts[index] != "" {
			parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
		}
	}
	return strings.Join(parts, "")
}
func harmonicSum(count int) float64 {
	total := 0.0
	for index := 1; index <= count; index++ {
		total += 1 / float64(index)
	}
	return total
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
	spec, ok := sound.LookupEffectSpec(effect.Kind)
	if !ok {
		return
	}
	for _, parameter := range spec.Parameters {
		value, exists := effect.Parameter(parameter.Name)
		if !exists {
			value = parameter.Default
		}
		fmt.Fprintf(text, "  chnset %s, %s\n", number(value), quote(effectChannel(index, parameter.Name)))
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

func writeOutputInstrument(text *strings.Builder, effects []sound.Effect, maxDelay time.Duration, masterGain float64, masterGainSet bool) {
	text.WriteString(`instr 1000
  aLeft = gaStashLeft
  aRight = gaStashRight
  clear gaStashLeft, gaStashRight
`)
	for index, effect := range effects {
		switch effect.Kind {
		case sound.EffectLowPass, sound.EffectHighPass, sound.EffectBandPass, sound.EffectNotch, sound.EffectPeak, sound.EffectLowShelf, sound.EffectHighShelf:
			mode := map[sound.EffectKind]int{sound.EffectLowPass: 0, sound.EffectHighPass: 1, sound.EffectBandPass: 2, sound.EffectNotch: 3, sound.EffectPeak: 4, sound.EffectLowShelf: 5, sound.EffectHighShelf: 6}[effect.Kind]
			fmt.Fprintf(text, "  kCutoff%d chnget %s\n", index, quote(effectChannel(index, "cutoff")))
			if effect.Kind == sound.EffectLowShelf || effect.Kind == sound.EffectHighShelf {
				fmt.Fprintf(text, "  kQ%d = %s\n", index, number(sound.DefaultFilterQ))
			} else {
				fmt.Fprintf(text, "  kQ%d chnget %s\n", index, quote(effectChannel(index, "q")))
			}
			if effect.Kind == sound.EffectPeak || effect.Kind == sound.EffectLowShelf || effect.Kind == sound.EffectHighShelf {
				fmt.Fprintf(text, "  kFilterGain%d chnget %s\n", index, quote(effectChannel(index, "gain")))
			} else {
				fmt.Fprintf(text, "  kFilterGain%d = 0\n", index)
			}
			fmt.Fprintf(text, "  kCutoff%d limit kCutoff%d, 1, (sr * 0.499)\n", index, index)
			fmt.Fprintf(text, "  aLeft rbjeq aLeft, kCutoff%d, kFilterGain%d, kQ%d, %d\n", index, index, index, mode)
			fmt.Fprintf(text, "  aRight rbjeq aRight, kCutoff%d, kFilterGain%d, kQ%d, %d\n", index, index, index, mode)
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
		case sound.EffectChorus:
			writeEffectChannels(text, index, "rate", "depth", "mix")
			fmt.Fprintf(text, "  aChorusLFO%d oscili (.001 + (kDepth%d * .014)), kRate%d, giStashSine\n", index, index, index)
			fmt.Fprintf(text, "  aChorusLeft%d vdelay3 aLeft, (aChorusLFO%d + .016) * 1000, 31\n", index, index)
			fmt.Fprintf(text, "  aChorusRight%d vdelay3 aRight, (-aChorusLFO%d + .016) * 1000, 31\n", index, index)
			writeWetMix(text, index, "aChorusLeft", "aChorusRight", "Mix")
		case sound.EffectFlanger:
			writeEffectChannels(text, index, "rate", "depth", "feedback", "mix")
			fmt.Fprintf(text, "  aFlangerLFO%d oscili kDepth%d * .5, kRate%d, giStashSine\n", index, index, index)
			fmt.Fprintf(text, "  aFlangerLeft%d flanger aLeft, (.0001 + (kDepth%d * .5) + aFlangerLFO%d), kFeedback%d, .1\n", index, index, index, index)
			fmt.Fprintf(text, "  aFlangerRight%d flanger aRight, (.0001 + (kDepth%d * .5) - aFlangerLFO%d), kFeedback%d, .1\n", index, index, index, index)
			writeWetMix(text, index, "aFlangerLeft", "aFlangerRight", "Mix")
		case sound.EffectPhaser:
			writeEffectChannels(text, index, "rate", "depth", "feedback", "stages")
			fmt.Fprintf(text, "  kPhaserFreq%d = 100 + ((.5 + (.5 * sin(2 * 3.141592653589793 * kRate%d * timeinsts()))) * kDepth%d * 3900)\n", index, index, index)
			fmt.Fprintf(text, "  aLeft phaser1 aLeft, kPhaserFreq%d, kStages%d, kFeedback%d\n", index, index, index)
			fmt.Fprintf(text, "  aRight phaser1 aRight, kPhaserFreq%d, kStages%d, kFeedback%d\n", index, index, index)
		case sound.EffectReverb:
			writeEffectChannels(text, index, "size", "damp", "mix")
			fmt.Fprintf(text, "  aReverbLeft%d, aReverbRight%d reverbsc aLeft, aRight, (.5 + (kSize%d * .49)), (1000 + ((1 - kDamp%d) * 17000))\n", index, index, index, index)
			writeWetMix(text, index, "aReverbLeft", "aReverbRight", "Mix")
		case sound.EffectTremolo:
			writeEffectChannels(text, index, "rate", "depth")
			fmt.Fprintf(text, "  kTremolo%d oscili 1, kRate%d, giStashSine\n  kTremoloGain%d = (1 - kDepth%d) + (kDepth%d * ((kTremolo%d + 1) * .5))\n", index, index, index, index, index, index)
			fmt.Fprintf(text, "  aLeft = aLeft * kTremoloGain%d\n  aRight = aRight * kTremoloGain%d\n", index, index)
		case sound.EffectPan:
			writeEffectChannels(text, index, "position", "rate", "depth")
			fmt.Fprintf(text, "  kPanLFO%d oscili kDepth%d, max(kRate%d, .000001), giStashSine\n  if (kRate%d <= 0) then\n    kPanLFO%d = 0\n  endif\n  kPanPosition%d limit (kPosition%d + kPanLFO%d), -1, 1\n", index, index, index, index, index, index, index, index)
			fmt.Fprintf(text, "  aPanMid%d = (aLeft + aRight) * .7071067811865476\n  aLeft = aPanMid%d * sqrt((1 - kPanPosition%d) * .5)\n  aRight = aPanMid%d * sqrt((1 + kPanPosition%d) * .5)\n", index, index, index, index, index)
		case sound.EffectWidth:
			writeEffectChannels(text, index, "amount")
			fmt.Fprintf(text, "  aWidthMid%d = (aLeft + aRight) * .5\n  aWidthSide%d = (aLeft - aRight) * .5 * kAmount%d\n  aLeft = aWidthMid%d + aWidthSide%d\n  aRight = aWidthMid%d - aWidthSide%d\n", index, index, index, index, index, index, index)
		case sound.EffectHaas:
			writeEffectChannels(text, index, "delay")
			fmt.Fprintf(text, "  aRight vdelay3 aRight, kDelay%d * 1000, 51\n", index)
		case sound.EffectCrush:
			writeEffectChannels(text, index, "bits", "rate")
			fmt.Fprintf(text, "  kCrushLevels%d = pow(2, kBits%d - 1)\n  aCrushLeft%d samphold aLeft, mpulse(1, 1 / max(kRate%d, 100))\n  aCrushRight%d samphold aRight, mpulse(1, 1 / max(kRate%d, 100))\n  aLeft = round(aCrushLeft%d * kCrushLevels%d) / kCrushLevels%d\n  aRight = round(aCrushRight%d * kCrushLevels%d) / kCrushLevels%d\n", index, index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectShape:
			writeEffectChannels(text, index, "drive", "bias")
			curve := effect.Config["curve"]
			if curve == "clip" {
				fmt.Fprintf(text, "  aLeft limit (aLeft * (1 + (kDrive%d * 20))) + kBias%d, -1, 1\n  aRight limit (aRight * (1 + (kDrive%d * 20))) + kBias%d, -1, 1\n", index, index, index, index)
			} else if curve == "atan" {
				fmt.Fprintf(text, "  aLeft = (2 / 3.141592653589793) * taninv((aLeft + kBias%d) * (1 + (kDrive%d * 20)))\n  aRight = (2 / 3.141592653589793) * taninv((aRight + kBias%d) * (1 + (kDrive%d * 20)))\n", index, index, index, index)
			} else {
				fmt.Fprintf(text, "  aLeft = tanh((aLeft + kBias%d) * (1 + (kDrive%d * 20)))\n  aRight = tanh((aRight + kBias%d) * (1 + (kDrive%d * 20)))\n", index, index, index, index)
			}
		case sound.EffectComb:
			writeEffectChannels(text, index, "delay", "feedback")
			fmt.Fprintf(text, "  aCombBufferLeft%d delayr 1\n  aCombLeft%d deltap3 max(kDelay%d, .0001)\n  delayw aLeft + (aCombLeft%d * kFeedback%d)\n  aCombBufferRight%d delayr 1\n  aCombRight%d deltap3 max(kDelay%d, .0001)\n  delayw aRight + (aCombRight%d * kFeedback%d)\n  aLeft = aLeft + aCombLeft%d\n  aRight = aRight + aCombRight%d\n", index, index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectAllpass:
			writeEffectChannels(text, index, "delay", "feedback")
			fmt.Fprintf(text, "  aAllpassBufferLeft%d delayr 1\n  aAllpassTapLeft%d deltap3 max(kDelay%d, .0001)\n  delayw aLeft + (aAllpassTapLeft%d * kFeedback%d)\n  aAllpassOutLeft%d = aAllpassTapLeft%d - (aLeft * kFeedback%d)\n  aAllpassBufferRight%d delayr 1\n  aAllpassTapRight%d deltap3 max(kDelay%d, .0001)\n  delayw aRight + (aAllpassTapRight%d * kFeedback%d)\n  aAllpassOutRight%d = aAllpassTapRight%d - (aRight * kFeedback%d)\n  aLeft = aAllpassOutLeft%d\n  aRight = aAllpassOutRight%d\n", index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectCompressor:
			writeEffectChannels(text, index, "threshold", "ratio", "attack", "release")
			fmt.Fprintf(text, "  aCompControl%d = max(abs(aLeft), abs(aRight))\n  aLeft compress aLeft, aCompControl%d, kThreshold%d, kThreshold%d + 3, 0, kRatio%d, kAttack%d, kRelease%d, .01\n  aRight compress aRight, aCompControl%d, kThreshold%d, kThreshold%d + 3, 0, kRatio%d, kAttack%d, kRelease%d, .01\n", index, index, index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectLimiter:
			writeEffectChannels(text, index, "threshold", "release")
			fmt.Fprintf(text, "  aLimitControl%d = max(abs(aLeft), abs(aRight))\n  aLeft compress aLeft, aLimitControl%d, kThreshold%d, kThreshold%d, 0, 100, .001, kRelease%d, .005\n  aRight compress aRight, aLimitControl%d, kThreshold%d, kThreshold%d, 0, 100, .001, kRelease%d, .005\n", index, index, index, index, index, index, index, index, index)
		case sound.EffectGate:
			writeEffectChannels(text, index, "threshold", "attack", "release")
			fmt.Fprintf(text, "  aGateEnvelope%d follow2 max(abs(aLeft), abs(aRight)), kAttack%d, kRelease%d\n  kGateEnvelope%d downsamp aGateEnvelope%d\n  if (kGateEnvelope%d >= ampdb(kThreshold%d)) then\n    kGateGain%d = 1\n  else\n    kGateGain%d = 0\n  endif\n  aLeft = aLeft * kGateGain%d\n  aRight = aRight * kGateGain%d\n", index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectResonator:
			writeEffectChannels(text, index, "freq", "q")
			fmt.Fprintf(text, "  aLeft reson aLeft, kFreq%d, max(kFreq%d / kQ%d, 1)\n  aRight reson aRight, kFreq%d, max(kFreq%d / kQ%d, 1)\n", index, index, index, index, index, index)
		case sound.EffectRing:
			writeEffectChannels(text, index, "freq", "mix")
			fmt.Fprintf(text, "  aRing%d oscili 1, kFreq%d, giStashSine\n  aLeft = (aLeft * (1 - kMix%d)) + (aLeft * aRing%d * kMix%d)\n  aRight = (aRight * (1 - kMix%d)) + (aRight * aRing%d * kMix%d)\n", index, index, index, index, index, index, index, index)
		case sound.EffectFrequencyShift:
			writeEffectChannels(text, index, "amount", "mix")
			fmt.Fprintf(text, "  aRealL%d, aImagL%d hilbert aLeft\n  aRealR%d, aImagR%d hilbert aRight\n  aShiftCos%d oscili 1, abs(kAmount%d), giStashSine, .25\n  aShiftSin%d oscili 1, abs(kAmount%d), giStashSine\n  aShiftLeft%d = (aRealL%d * aShiftCos%d) - (aImagL%d * aShiftSin%d * (kAmount%d >= 0 ? 1 : -1))\n  aShiftRight%d = (aRealR%d * aShiftCos%d) - (aImagR%d * aShiftSin%d * (kAmount%d >= 0 ? 1 : -1))\n", index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index)
			writeWetMix(text, index, "aShiftLeft", "aShiftRight", "Mix")
		case sound.EffectFold:
			writeEffectChannels(text, index, "amount")
			fmt.Fprintf(text, "  aLeft = (2 / 3.141592653589793) * sininv(sin(aLeft * (1 + (kAmount%d * 8))))\n  aRight = (2 / 3.141592653589793) * sininv(sin(aRight * (1 + (kAmount%d * 8))))\n", index, index)
		case sound.EffectFormant:
			writeEffectChannels(text, index, "position")
			fmt.Fprintf(text, "  kFormant1_%d = 800 - (530 * kPosition%d)\n  kFormant2_%d = 1150 + (1140 * kPosition%d)\n  aFormantL1_%d reson aLeft, kFormant1_%d, 100\n  aFormantL2_%d reson aLeft, kFormant2_%d, 140\n  aFormantR1_%d reson aRight, kFormant1_%d, 100\n  aFormantR2_%d reson aRight, kFormant2_%d, 140\n  aLeft = (aFormantL1_%d + aFormantL2_%d) * .5\n  aRight = (aFormantR1_%d + aFormantR2_%d) * .5\n", index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectPitch:
			writeEffectChannels(text, index, "semitones", "mix")
			fmt.Fprintf(text, "  fPitchLeft%d pvsanal aLeft, 1024, 256, 1024, 1\n  fPitchRight%d pvsanal aRight, 1024, 256, 1024, 1\n  fPitchScaledLeft%d pvscale fPitchLeft%d, pow(2, kSemitones%d / 12)\n  fPitchScaledRight%d pvscale fPitchRight%d, pow(2, kSemitones%d / 12)\n  aPitchLeft%d pvsynth fPitchScaledLeft%d\n  aPitchRight%d pvsynth fPitchScaledRight%d\n", index, index, index, index, index, index, index, index, index, index, index, index)
			writeWetMix(text, index, "aPitchLeft", "aPitchRight", "Mix")
		case sound.EffectFreeze:
			writeEffectChannels(text, index, "amount")
			fmt.Fprintf(text, "  fFreezeLeft%d pvsanal aLeft, 1024, 256, 1024, 1\n  fFreezeRight%d pvsanal aRight, 1024, 256, 1024, 1\n  fFrozenLeft%d pvsfreeze fFreezeLeft%d, kAmount%d, kAmount%d\n  fFrozenRight%d pvsfreeze fFreezeRight%d, kAmount%d, kAmount%d\n  aLeft pvsynth fFrozenLeft%d\n  aRight pvsynth fFrozenRight%d\n", index, index, index, index, index, index, index, index, index, index, index, index)
		case sound.EffectSpectralBlur:
			writeEffectChannels(text, index, "amount", "mix")
			fmt.Fprintf(text, "  fBlurLeft%d pvsanal aLeft, 1024, 256, 1024, 1\n  fBlurRight%d pvsanal aRight, 1024, 256, 1024, 1\n  fBlurredLeft%d pvsblur fBlurLeft%d, kAmount%d, 2\n  fBlurredRight%d pvsblur fBlurRight%d, kAmount%d, 2\n  aBlurLeft%d pvsynth fBlurredLeft%d\n  aBlurRight%d pvsynth fBlurredRight%d\n", index, index, index, index, index, index, index, index, index, index, index, index)
			writeWetMix(text, index, "aBlurLeft", "aBlurRight", "Mix")
		case sound.EffectSpectralShift:
			writeEffectChannels(text, index, "amount", "mix")
			fmt.Fprintf(text, "  fSpecShiftLeft%d pvsanal aLeft, 1024, 256, 1024, 1\n  fSpecShiftRight%d pvsanal aRight, 1024, 256, 1024, 1\n  fShiftedLeft%d pvshift fSpecShiftLeft%d, kAmount%d, 0\n  fShiftedRight%d pvshift fSpecShiftRight%d, kAmount%d, 0\n  aSpecShiftLeft%d pvsynth fShiftedLeft%d\n  aSpecShiftRight%d pvsynth fShiftedRight%d\n", index, index, index, index, index, index, index, index, index, index, index, index)
			writeWetMix(text, index, "aSpecShiftLeft", "aSpecShiftRight", "Mix")
		case sound.EffectStutter:
			writeEffectChannels(text, index, "size", "repeats", "prob")
			fmt.Fprintf(text, "  kStutterPhase%d phasor 1 / max(kSize%d * kRepeats%d, .001)\n  kStutterSlice%d = frac(kStutterPhase%d * kRepeats%d)\n  kStutterTap%d = max(kSize%d * (1 - kStutterSlice%d), .0001)\n  aStutterLeft%d vdelay3 aLeft, kStutterTap%d * 1000, 2001\n  aStutterRight%d vdelay3 aRight, kStutterTap%d * 1000, 2001\n  if (kRepeats%d > 1) then\n    kStutterMix%d = kProb%d\n  else\n    kStutterMix%d = 0\n  endif\n", index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index, index)
			writeWetMix(text, index, "aStutterLeft", "aStutterRight", "StutterMix")
		case sound.EffectGranular:
			writeEffectChannels(text, index, "size", "density", "jitter", "pitch", "mix")
			fmt.Fprintf(text, "  kGrainJitter%d randomi -kJitter%d, kJitter%d, max(kDensity%d, .1)\n  kGrainDelay%d = max(kSize%d * (1 + kGrainJitter%d) / max(kPitch%d, .125), .0001)\n  aGrainLeft%d vdelay3 aLeft, kGrainDelay%d * 1000, 1001\n  aGrainRight%d vdelay3 aRight, kGrainDelay%d * 1000, 1001\n", index, index, index, index, index, index, index, index, index, index, index, index)
			writeWetMix(text, index, "aGrainLeft", "aGrainRight", "Mix")
		case sound.EffectConvolution:
			writeEffectChannels(text, index, "mix")
			fmt.Fprintf(text, "  aConvLeft%d pconvolve aLeft, %s\n  aConvRight%d pconvolve aRight, %s\n", index, quote(effect.Config["impulse"]), index, quote(effect.Config["impulse"]))
			writeWetMix(text, index, "aConvLeft", "aConvRight", "Mix")
		}
	}
	if !masterGainSet {
		masterGain = 1
	}
	fmt.Fprintf(text, "  aLeft = aLeft * %s\n  aRight = aRight * %s\n", number(masterGain), number(masterGain))
	text.WriteString("  outs aLeft, aRight\nendin\n\n")
}

func writeEffectChannels(text *strings.Builder, index int, names ...string) {
	for _, name := range names {
		fmt.Fprintf(text, "  k%s%d chnget %s\n", csName(name), index, quote(effectChannel(index, name)))
	}
}

func writeWetMix(text *strings.Builder, index int, left, right, mix string) {
	fmt.Fprintf(text, "  aLeft = (aLeft * (1 - k%s%d)) + (%s%d * k%s%d)\n", mix, index, left, index, mix, index)
	fmt.Fprintf(text, "  aRight = (aRight * (1 - k%s%d)) + (%s%d * k%s%d)\n", mix, index, right, index, mix, index)
}

func voiceInstrument(index int) int {
	return 100 + index
}

func synthInstrument(index int) int { return 500 + index }

func voiceChannel(index int, parameter string) string {
	return fmt.Sprintf("voice.%d.%s", index, parameter)
}

func synthChannel(index int, parameter string) string {
	return fmt.Sprintf("synth.%d.%s", index, parameter)
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
