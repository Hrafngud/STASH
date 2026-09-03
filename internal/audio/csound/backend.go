// Package csound implements STASH's audio backend with the Csound command-line
// runtime. Orchestra and score syntax stay confined to this internal package.
package csound

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/sound"
)

var versionPattern = regexp.MustCompile(`(?i)csound version\s+([0-9]+)`) // Csound 6 and 7 output.

// Backend starts an external Csound process. Executable defaults to "csound".
type Backend struct {
	Executable string
}

var _ audio.Backend = (*Backend)(nil)

// New returns a Csound backend using executable. An empty name searches PATH
// for the standard "csound" command.
func New(executable string) *Backend {
	return &Backend{Executable: executable}
}

// Start validates Csound and the generated orchestra before starting a
// persistent process. No diagnostic output is written by this package itself.
func (backend *Backend) Start(ctx context.Context, config audio.Config) (audio.Session, error) {
	if ctx == nil {
		return nil, fmt.Errorf("csound startup: context is nil")
	}
	document, maxDelay, err := orchestra(config)
	if err != nil {
		return nil, fmt.Errorf("csound startup: %w", err)
	}
	executable, err := backend.resolveExecutable(ctx)
	if err != nil {
		return nil, err
	}
	path, err := writeOrchestra(document)
	if err != nil {
		return nil, fmt.Errorf("csound startup: create orchestra: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(path)
		}
	}()

	if err := preflight(ctx, executable, path); err != nil {
		return nil, err
	}
	arguments := commandArguments(config.Output, path)
	command := exec.CommandContext(ctx, executable, arguments...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("csound startup: open control input: %w", err)
	}
	if config.Output == audio.OutputRawPCM {
		command.Stdout = newRealtimePCMWriter(ctx, config.PCM)
	} else {
		command.Stdout = io.Discard
	}
	if config.Diagnostics == nil {
		command.Stderr = io.Discard
	} else {
		command.Stderr = config.Diagnostics
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("csound startup: %w", err)
	}

	session := &processSession{
		context:   ctx,
		command:   command,
		stdin:     stdin,
		model:     cloneModel(config.Model),
		maxDelay:  maxDelay,
		orchestra: path,
		done:      make(chan struct{}),
	}
	cleanup = false
	go session.reap()
	return session, nil
}

const rawPCMBytesPerSecond = audio.SampleRate * audio.Channels * 4

type realtimePCMWriter struct {
	ctx     context.Context
	output  io.Writer
	started time.Time
	written int64
	now     func() time.Time
	wait    func(context.Context, time.Duration) error
}

func newRealtimePCMWriter(ctx context.Context, output io.Writer) *realtimePCMWriter {
	return &realtimePCMWriter{
		ctx:    ctx,
		output: output,
		now:    time.Now,
		wait: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

// Write backpressures Csound's offline stdout renderer to the public PCM
// sample rate. Without pacing, file output outruns live telemetry and can fill
// a downstream player's buffers with minutes of stale control state.
func (writer *realtimePCMWriter) Write(data []byte) (int, error) {
	if writer.started.IsZero() {
		writer.started = writer.now()
	}
	written, err := writer.output.Write(data)
	writer.written += int64(written)
	if err != nil || written != len(data) {
		return written, err
	}
	target := writer.started.Add(rawPCMDuration(writer.written))
	if delay := target.Sub(writer.now()); delay > 0 {
		if err := writer.wait(writer.ctx, delay); err != nil {
			return written, err
		}
	}
	return written, nil
}

func rawPCMDuration(byteCount int64) time.Duration {
	rate := int64(rawPCMBytesPerSecond)
	seconds, remainder := byteCount/rate, byteCount%rate
	return time.Duration(seconds)*time.Second + time.Duration(remainder)*time.Second/time.Duration(rate)
}

func (backend *Backend) resolveExecutable(ctx context.Context) (string, error) {
	name := backend.Executable
	if name == "" {
		name = "csound"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("csound unavailable: executable %q not found", name)
	}
	command := exec.CommandContext(ctx, path, "--version")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("csound incompatible: version check failed: %s", outputSummary(output, err.Error()))
	}
	match := versionPattern.FindSubmatch(output)
	if len(match) != 2 {
		return "", fmt.Errorf("csound incompatible: unrecognized version output")
	}
	major, err := strconv.Atoi(string(match[1]))
	if err != nil || major < 6 {
		return "", fmt.Errorf("csound incompatible: version 6 or newer is required")
	}
	return path, nil
}

func writeOrchestra(document string) (string, error) {
	file, err := os.CreateTemp("", "stash-csound-*.csd")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := io.WriteString(file, document); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func preflight(ctx context.Context, executable, path string) error {
	command := exec.CommandContext(ctx, executable, "-n", "-d", "-m0", "--syntax-check-only", path)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("csound incompatible: orchestra validation failed: %s", outputSummary(output, err.Error()))
	}
	return nil
}

func commandArguments(output audio.OutputKind, orchestraPath string) []string {
	arguments := []string{"-d", "-m0"}
	if output == audio.OutputRawPCM {
		arguments = append(arguments, "-h", "-f", "-o", "stdout")
	} else {
		arguments = append(arguments, "-odac")
	}
	return append(arguments, "-L", "stdin", orchestraPath)
}

type processSession struct {
	context context.Context
	command *exec.Cmd
	stdin   io.WriteCloser

	mu        sync.Mutex
	model     sound.Model
	maxDelay  time.Duration
	closed    bool
	waitError error

	orchestra string
	done      chan struct{}
}

var _ audio.Session = (*processSession)(nil)

func (session *processSession) reap() {
	err := session.command.Wait()
	if session.context.Err() != nil {
		err = session.context.Err()
	}
	session.mu.Lock()
	session.waitError = err
	session.closed = true
	session.mu.Unlock()
	_ = os.Remove(session.orchestra)
	close(session.done)
}

func (session *processSession) Update(ctx context.Context, update audio.Update) error {
	if ctx == nil {
		return fmt.Errorf("csound update: context is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
		if err := session.Wait(); err != nil {
			return fmt.Errorf("csound update: renderer stopped: %w", err)
		}
		return fmt.Errorf("csound update: renderer stopped")
	default:
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return fmt.Errorf("csound update: renderer is closed")
	}
	if update.Target.Name == "delay.time" && update.Value > session.maxDelay.Seconds() {
		return fmt.Errorf("csound update: delay time %gs exceeds configured maximum %s", update.Value, session.maxDelay)
	}
	next := cloneModel(session.model)
	if err := update.Target.Set(&next, update.VoiceIndex, update.Value); err != nil {
		return fmt.Errorf("csound update: %w", err)
	}
	channel, err := channelForTarget(update.Target, update.VoiceIndex)
	if err != nil {
		return fmt.Errorf("csound update: %w", err)
	}
	channelValue := update.Value
	if update.Target.IsSynth {
		synth := next.Synths[update.Target.SynthIndex]
		name := update.Target.Name
		if update.Target.Vector {
			name = fmt.Sprintf("partial.%d.gain", update.VoiceIndex)
		}
		channelValue = synth.Parameters[name] + synth.Modulations[name]
	} else if update.Target.EffectIndex >= 0 {
		channelValue, err = update.Target.Value(next, update.VoiceIndex)
		if err != nil {
			return fmt.Errorf("csound update: %w", err)
		}
	}
	event := fmt.Sprintf("i 2 0 0.001 %s %s\n", quote(channel), number(channelValue))
	if _, err := io.WriteString(session.stdin, event); err != nil {
		return fmt.Errorf("csound update: write control channel: %w", err)
	}
	session.model = next
	return nil
}

func (session *processSession) Wait() error {
	<-session.done
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.waitError != nil {
		return fmt.Errorf("csound renderer: %w", session.waitError)
	}
	return nil
}

func (session *processSession) Close() error {
	session.mu.Lock()
	if !session.closed {
		session.closed = true
		_, writeErr := io.WriteString(session.stdin, "e\n")
		closeErr := session.stdin.Close()
		session.mu.Unlock()
		waitErr := session.Wait()
		if writeErr != nil && !errors.Is(writeErr, os.ErrClosed) {
			return fmt.Errorf("csound shutdown: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("csound shutdown: %w", closeErr)
		}
		return waitErr
	}
	session.mu.Unlock()
	return session.Wait()
}

func channelForTarget(target sound.Target, voiceIndex int) (string, error) {
	if target.IsSynth {
		if target.SynthIndex < 0 {
			return "", fmt.Errorf("synth index %d out of range", target.SynthIndex)
		}
		name := target.Name
		if target.Vector {
			name = fmt.Sprintf("partial.%d.gain", voiceIndex)
		}
		return synthChannel(target.SynthIndex, name), nil
	}
	if target.EffectIndex < 0 {
		switch target.Name {
		case "freq", "gain", "pan", "gate":
			if voiceIndex < 0 {
				return "", fmt.Errorf("voice index %d out of range", voiceIndex)
			}
			return voiceChannel(voiceIndex, target.Name), nil
		default:
			return "", fmt.Errorf("unknown voice target %q", target.Name)
		}
	}
	separator := strings.LastIndexByte(target.Name, '.')
	if separator < 0 || separator == len(target.Name)-1 {
		return "", fmt.Errorf("unknown effect target %q", target.Name)
	}
	parameter := target.Name[separator+1:]
	return effectChannel(target.EffectIndex, parameter), nil
}

func cloneModel(model sound.Model) sound.Model {
	cloned := sound.Model{
		Voices:      append([]sound.Voice(nil), model.Voices...),
		Effects:     append([]sound.Effect(nil), model.Effects...),
		AudioRoutes: append([]sound.AudioRoute(nil), model.AudioRoutes...), MasterGain: model.MasterGain, MasterGainSet: model.MasterGainSet,
	}
	cloned.Synths = make([]sound.Synth, len(model.Synths))
	for index, synth := range model.Synths {
		cloned.Synths[index] = synth
		cloned.Synths[index].Parameters = map[string]float64{}
		for name, value := range synth.Parameters {
			cloned.Synths[index].Parameters[name] = value
		}
		cloned.Synths[index].Modulations = map[string]float64{}
		for name, value := range synth.Modulations {
			cloned.Synths[index].Modulations[name] = value
		}
		cloned.Synths[index].Config = map[string]string{}
		for name, value := range synth.Config {
			cloned.Synths[index].Config[name] = value
		}
		cloned.Synths[index].Explicit = map[string]bool{}
		for name, value := range synth.Explicit {
			cloned.Synths[index].Explicit[name] = value
		}
	}
	for index := range cloned.Effects {
		cloned.Effects[index].Parameters = map[string]float64{}
		for name, value := range model.Effects[index].Parameters {
			cloned.Effects[index].Parameters[name] = value
		}
		cloned.Effects[index].Config = map[string]string{}
		for name, value := range model.Effects[index].Config {
			cloned.Effects[index].Config[name] = value
		}
	}
	return cloned
}

func outputSummary(output []byte, fallback string) string {
	plain := stripANSI(output)
	plain = strings.Join(strings.Fields(plain), " ")
	if plain == "" {
		plain = fallback
	}
	const limit = 240
	if len(plain) > limit {
		plain = plain[:limit] + "..."
	}
	return plain
}

func stripANSI(input []byte) string {
	var output bytes.Buffer
	for index := 0; index < len(input); index++ {
		if input[index] == 0x1b && index+1 < len(input) && input[index+1] == '[' {
			index += 2
			for index < len(input) && (input[index] < '@' || input[index] > '~') {
				index++
			}
			continue
		}
		output.WriteByte(input[index])
	}
	return output.String()
}
