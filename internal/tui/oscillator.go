package tui

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/zalmo/stash/internal/sound"
)

type oscillatorSignal struct {
	wave      sound.Waveform
	frequency float64
	gain      float64
	label     string
	synth     *sound.Synth
}

func (state *editor) oscillatorPanel(width, height int) string {
	innerWidth := max(24, width-4)
	innerHeight := max(1, height-3)
	signal := state.currentOscillatorSignal()

	titleLeft := sectionStyle.Render("OSCILLATOR")
	titleRight := mutedStyle.Render(signal.label)
	titleGap := innerWidth - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if titleGap < 1 {
		titleGap = 1
		titleRight = ""
	}
	title := titleLeft + strings.Repeat(" ", titleGap) + titleRight
	body := oscillatorDots(signal, innerWidth, innerHeight, state.oscillatorFrame, state.muted)
	return panelStyle.Width(width).Height(height).Render(title + "\n" + body)
}

func (state *editor) currentOscillatorSignal() oscillatorSignal {
	signal := oscillatorSignal{
		wave: sound.WaveSine, frequency: 440, gain: .1,
		label: "WAITING FOR SIGNAL",
	}
	if state.lastValid == nil {
		return signal
	}

	model := state.lastValid.Sound
	if len(model.Synths) > 0 {
		synth := &model.Synths[0]
		signal.synth = synth
		if wave := synth.Config["wave"]; wave != "" {
			signal.wave = sound.Waveform(wave)
		}
		if frequency := synth.Parameters["freq"]; frequency > 0 {
			signal.frequency = frequency
		}
		signal.gain = synth.Parameters["gain"]
		if model.MasterGainSet {
			signal.gain *= model.MasterGain
		}
		signal.label = strings.ToUpper(fmt.Sprintf("%s:%s / %s", synth.Type, synth.ID, signal.wave))
		return signal
	}
	if len(model.Voices) > 0 {
		voice := model.Voices[0]
		signal.wave, signal.frequency, signal.gain = voice.Waveform, voice.Frequency, voice.Gain
		signal.label = strings.ToUpper(fmt.Sprintf("VOICE / %s", voice.Waveform))
	}
	return signal
}

func oscillatorDots(signal oscillatorSignal, width, height int, frame uint64, muted bool) string {
	width, height = max(1, width), max(1, height)
	grid := make([][]byte, height)
	for row := range grid {
		grid[row] = []byte(strings.Repeat(" ", width))
	}

	phase := float64(frame) * .24
	cycles := math.Log2(max(signal.frequency, 55) / 55)
	cycles = min(4, max(1, cycles))
	amplitude := .42 + .42*math.Sqrt(min(1, max(0, signal.gain)))
	if muted || signal.label == "WAITING FOR SIGNAL" {
		amplitude = 0
	}
	for column := 0; column < width; column++ {
		position := float64(column) / float64(max(1, width-1))
		angle := 2*math.Pi*cycles*position + phase
		value := oscillatorWave(signal, angle, column, frame)
		row := int(math.Round(float64(height-1) * (.5 - value*amplitude*.5)))
		row = min(height-1, max(0, row))
		grid[row][column] = '.'
	}

	lines := make([]string, height)
	for row := range grid {
		lines[row] = string(grid[row])
	}
	return strings.Join(lines, "\n")
}

func oscillatorWave(signal oscillatorSignal, angle float64, column int, frame uint64) float64 {
	if signal.synth != nil {
		synth := signal.synth
		switch synth.Type {
		case sound.SynthFM, sound.SynthPM:
			ratio := max(.01, synth.Parameters["ratio"])
			index := min(10, synth.Parameters["index"])
			angle += math.Sin(angle*ratio+float64(frame)*.07) * index * .22
		case sound.SynthAM:
			ratio := max(.01, synth.Parameters["ratio"])
			depth := min(1, max(0, synth.Parameters["depth"]))
			return oscillatorWaveform(signal.wave, angle) * ((1 - depth) + depth*(math.Sin(angle*ratio)+1)/2)
		case sound.SynthRing:
			ratio := max(.01, synth.Parameters["ratio"])
			return oscillatorWaveform(signal.wave, angle) * math.Sin(angle*ratio)
		}
	}
	return oscillatorWaveform(signal.wave, angle+noiseJitter(column, frame, signal.wave))
}

func oscillatorWaveform(wave sound.Waveform, angle float64) float64 {
	cycle := angle / (2 * math.Pi)
	fraction := cycle - math.Floor(cycle)
	switch wave {
	case sound.WaveSquare:
		if math.Sin(angle) >= 0 {
			return 1
		}
		return -1
	case sound.WaveSaw:
		return 2*fraction - 1
	case sound.WaveTri:
		return 1 - 4*math.Abs(fraction-.5)
	case sound.WaveNoise:
		return noiseSample(int64(math.Floor(cycle*24)), 0)
	default:
		return math.Sin(angle)
	}
}

func noiseJitter(column int, frame uint64, wave sound.Waveform) float64 {
	if wave != sound.WaveNoise {
		return 0
	}
	return noiseSample(int64(column), frame) * math.Pi
}

func noiseSample(index int64, frame uint64) float64 {
	value := uint64(index) + frame*0x9e3779b97f4a7c15
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	value ^= value >> 31
	return float64(value%2001)/1000 - 1
}
