package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/zalmo/stash/internal/audio"
	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/source"
	"github.com/zalmo/stash/internal/unit"
)

// Config contains the process-owned dependencies used by the editor.
type Config struct {
	Registry       *source.Registry
	Backend        audio.Backend
	Input          io.Reader
	Output         io.Writer
	Diagnostics    io.Writer
	PresetDir      string
	SampleInterval func(string) time.Duration
	RhythmInterval time.Duration
	MaxDelay       time.Duration
}

type editor struct {
	config          Config
	lines           []string
	active          int
	editing         bool
	input           textinput.Model
	suggestions     []Suggestion
	selected        int
	analysis        Analysis
	lastValid       *cli.Plan
	message         string
	width           int
	height          int
	muted           bool
	oscillator      bool
	oscillatorFrame uint64
	enhancedKeys    bool
	numberChosen    bool
	chosenNumber    numericRange
	live            *liveEngine
	backendLog      *diagnosticCapture
	presetMode      presetMode
	presetInput     textinput.Model
	presets         []preset
	presetSelected  int
	presetOverwrite bool
}

type presetMode uint8

const (
	presetClosed presetMode = iota
	presetSaving
	presetLoading
)

type runtimeTick time.Time

const runtimePollInterval = 100 * time.Millisecond

var (
	textColor    = adaptiveColor("#202124", "#ECECF1")
	mutedColor   = adaptiveColor("#5F6368", "#A7A9B4")
	borderColor  = adaptiveColor("#B8BABF", "#555863")
	surfaceColor = adaptiveColor("#E8E9EC", "#30323A")
	inverseColor = adaptiveColor("#F7F7F5", "#17181C")

	logoStyle             = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	subtitleStyle         = lipgloss.NewStyle().Foreground(mutedColor)
	sectionStyle          = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	panelStyle            = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderColor).Padding(0, 1)
	activeLineStyle       = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	mutedStyle            = lipgloss.NewStyle().Foreground(mutedColor)
	selectedStyle         = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(textColor)
	numericSelectionStyle = lipgloss.NewStyle().Bold(true).Foreground(inverseColor).Background(textColor)
	helpKeyStyle          = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	helpTextStyle         = lipgloss.NewStyle().Foreground(mutedColor)
	validStyle            = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	incompleteStyle       = lipgloss.NewStyle().Foreground(textColor)
	invalidStyle          = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(textColor)
	modeStyle             = lipgloss.NewStyle().Bold(true).Foreground(textColor).Background(surfaceColor).Padding(0, 1)
	errorDetailStyle      = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(textColor)
	labelStyle            = lipgloss.NewStyle().Bold(true).Foreground(textColor)
)

func adaptiveColor(light, dark string) compat.AdaptiveColor {
	return compat.AdaptiveColor{Light: lipgloss.Color(light), Dark: lipgloss.Color(dark)}
}

// Run opens the Bubble Tea instrument editor. Ctrl-G saves the current
// instrument as a preset; q exits.
func Run(ctx context.Context, config Config) error {
	if ctx == nil {
		return fmt.Errorf("open TUI: context is nil")
	}
	if config.Registry == nil || config.Input == nil || config.Output == nil || config.Diagnostics == nil {
		return fmt.Errorf("open TUI: registry, input, output, and diagnostics are required")
	}
	if config.PresetDir == "" {
		var err error
		config.PresetDir, err = defaultPresetDir()
		if err != nil {
			return err
		}
	}

	state := newEditor(ctx, config)
	defer state.live.close()
	program := tea.NewProgram(
		state,
		tea.WithContext(ctx),
		tea.WithInput(config.Input),
		tea.WithOutput(config.Output),
		tea.WithoutSignalHandler(),
	)
	_, runErr := program.Run()
	if runErr != nil {
		if errors.Is(runErr, tea.ErrProgramKilled) && ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(runErr, tea.ErrInterrupted) {
			return nil
		}
		return runErr
	}
	return nil
}

func newEditor(ctx context.Context, config Config) *editor {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "type a source or option"
	input.CharLimit = 0
	styles := input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(textColor)
	styles.Focused.Placeholder = mutedStyle.Italic(true)
	styles.Cursor.Color = textColor
	input.SetStyles(styles)
	presetInput := textinput.New()
	presetInput.Prompt = ""
	presetInput.Placeholder = "preset name"
	presetInput.CharLimit = 64
	presetStyles := presetInput.Styles()
	presetStyles.Focused.Text = lipgloss.NewStyle().Foreground(textColor)
	presetStyles.Focused.Placeholder = mutedStyle.Italic(true)
	presetStyles.Cursor.Color = textColor
	presetInput.SetStyles(presetStyles)

	state := &editor{
		config: config, lines: initialLines(config.Registry), input: input, presetInput: presetInput,
		width: 80, height: 24, backendLog: &diagnosticCapture{},
	}
	state.live = &liveEngine{
		parent: ctx, registry: config.Registry, backend: config.Backend, diagnostics: state.backendLog,
		sampleInterval: config.SampleInterval, rhythmInterval: config.RhythmInterval, maxDelay: config.MaxDelay,
	}
	state.refresh(true)
	return state
}

func (state *editor) Init() tea.Cmd {
	return pollRuntimeCmd()
}

func pollRuntimeCmd() tea.Cmd {
	return tea.Tick(runtimePollInterval, func(now time.Time) tea.Msg { return runtimeTick(now) })
}

func (state *editor) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		state.width, state.height = message.Width, message.Height
		state.resizeInput()
		return state, nil
	case runtimeTick:
		state.pollRuntime()
		if state.oscillator && !state.muted && state.lastValid != nil {
			state.oscillatorFrame++
		}
		return state, pollRuntimeCmd()
	case tea.KeyboardEnhancementsMsg:
		state.enhancedKeys = message.SupportsKeyDisambiguation()
		return state, nil
	}
	if state.presetMode != presetClosed {
		return state, state.updatePreset(message)
	}
	switch message := message.(type) {
	case tea.PasteMsg:
		if state.importCommandPaste(message.Content) {
			return state, nil
		}
	case tea.KeyPressMsg:
		return state.updateKey(message)
	}
	if state.editing {
		before := state.input.Value()
		if _, pasted := message.(tea.PasteMsg); pasted {
			state.removeChosenNumber()
		}
		var command tea.Cmd
		state.input, command = state.input.Update(message)
		if state.input.Value() != before {
			state.lines[state.active] = state.input.Value()
			state.selected = 0
			state.refresh(true)
		}
		return state, command
	}
	return state, nil
}

func (state *editor) importCommandPaste(content string) bool {
	if !isCommandPaste(content) {
		return false
	}
	lines, err := pastedCommandLines(content)
	if err != nil {
		state.message = "paste: " + err.Error()
		return true
	}
	state.lines = lines
	state.active = 0
	state.editing = false
	state.selected = 0
	state.numberChosen = false
	state.input.Blur()
	state.message = ""
	state.refresh(true)
	return true
}

func (state *editor) updateKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key = printableKey(key)
	state.message = ""
	name := key.String()
	if name == "ctrl+c" || !state.editing && (name == "q" || name == "esc") {
		return state, tea.Quit
	}
	if name == "ctrl+shift+g" || name == "alt+g" {
		state.openPresetPicker()
		return state, nil
	}
	if name == "ctrl+g" {
		if state.analysis.State != Valid {
			state.message = "finish the instrument before saving a preset"
			return state, nil
		}
		state.openPresetSave()
		return state, nil
	}
	if name == "ctrl+m" || name == "alt+m" {
		state.toggleMute()
		return state, nil
	}
	if name == "ctrl+o" {
		state.oscillator = !state.oscillator
		return state, nil
	}
	if name == "ctrl+shift+d" || name == "alt+d" {
		state.deleteActive()
		return state, nil
	}
	if state.editing && name == "ctrl+d" {
		state.finishEditing()
		return state, nil
	}
	if name == "ctrl+n" {
		return state, state.insert(state.active + 1)
	}
	if name == "ctrl+p" {
		return state, state.insert(state.active)
	}
	if state.editing {
		return state, state.updateEdit(key)
	}
	return state, state.updateNormal(name)
}

func (state *editor) openPresetSave() {
	if state.editing {
		state.finishEditing()
	}
	state.presetMode = presetSaving
	state.presetInput.SetValue("")
	state.presetInput.CursorEnd()
	state.presetOverwrite = false
	state.message = ""
	state.presetInput.Focus()
}

func (state *editor) openPresetPicker() {
	if state.editing {
		state.finishEditing()
	}
	presets, err := listPresets(state.config.PresetDir)
	if err != nil {
		state.message = err.Error()
		return
	}
	state.presets = presets
	state.presetSelected = 0
	state.presetMode = presetLoading
	state.message = ""
	state.input.Blur()
}

func (state *editor) closePreset() tea.Cmd {
	state.presetMode = presetClosed
	state.presetOverwrite = false
	state.presetInput.Blur()
	return nil
}

func (state *editor) updatePreset(message tea.Msg) tea.Cmd {
	key, isKey := message.(tea.KeyPressMsg)
	if !isKey {
		if state.presetMode == presetSaving {
			var command tea.Cmd
			state.presetInput, command = state.presetInput.Update(message)
			return command
		}
		return nil
	}
	key = printableKey(key)
	name := key.String()
	if name == "esc" || name == "ctrl+c" {
		state.message = "preset action cancelled"
		return state.closePreset()
	}

	if state.presetMode == presetLoading {
		switch name {
		case "up", "k", "shift+tab":
			if len(state.presets) > 0 {
				state.presetSelected = (state.presetSelected - 1 + len(state.presets)) % len(state.presets)
			}
		case "down", "j", "tab":
			if len(state.presets) > 0 {
				state.presetSelected = (state.presetSelected + 1) % len(state.presets)
			}
		case "enter":
			if len(state.presets) == 0 {
				return nil
			}
			item := state.presets[state.presetSelected]
			lines, err := loadPreset(item)
			if err != nil {
				state.message = err.Error()
				return nil
			}
			state.lines = lines
			state.active = 0
			state.selected = 0
			state.numberChosen = false
			state.closePreset()
			state.refresh(true)
			state.message = fmt.Sprintf("loaded preset %q", item.name)
		}
		return nil
	}

	if name == "enter" {
		presetName, err := presetName(state.presetInput.Value())
		if err != nil {
			state.message = err.Error()
			return nil
		}
		path := filepath.Join(state.config.PresetDir, presetName+presetExtension)
		if _, err := os.Stat(path); err == nil && !state.presetOverwrite {
			state.presetOverwrite = true
			state.message = "preset already exists; press enter again to replace it"
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			state.message = "inspect preset: " + err.Error()
			return nil
		}
		savedName, replaced, err := savePreset(state.config.PresetDir, presetName, Command(state.lines))
		if err != nil {
			state.message = err.Error()
			return nil
		}
		state.closePreset()
		verb := "saved"
		if replaced {
			verb = "updated"
		}
		state.message = fmt.Sprintf("%s preset %q", verb, savedName)
		return nil
	}

	before := state.presetInput.Value()
	var command tea.Cmd
	state.presetInput, command = state.presetInput.Update(key)
	if state.presetInput.Value() != before {
		state.presetOverwrite = false
		state.message = ""
	}
	return command
}

func (state *editor) updateNormal(key string) tea.Cmd {
	switch key {
	case "up", "k":
		if state.active > 0 {
			state.active--
		}
	case "down", "j":
		if state.active+1 < len(state.lines) {
			state.active++
		}
	case "enter", "e":
		return state.startEditing()
	case "ctrl+up":
		if state.active > 0 {
			state.lines[state.active], state.lines[state.active-1] = state.lines[state.active-1], state.lines[state.active]
			state.active--
			state.refresh(true)
		}
	case "ctrl+down":
		if state.active+1 < len(state.lines) {
			state.lines[state.active], state.lines[state.active+1] = state.lines[state.active+1], state.lines[state.active]
			state.active++
			state.refresh(true)
		}
	case "a", "o":
		return state.insert(state.active + 1)
	}
	return nil
}

func (state *editor) updateEdit(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "up", "shift+tab":
		state.selectSuggestion(-1)
		return nil
	case "down", "tab":
		state.selectSuggestion(1)
		return nil
	case "enter":
		if len(state.suggestions) == 0 {
			state.finishEditing()
			return nil
		}
		state.input.SetValue(state.suggestions[state.selected].Value)
		state.input.CursorEnd()
		state.focusNearestNumber()
		state.lines[state.active] = state.input.Value()
		state.selected = 0
		state.refresh(true)
		return nil
	case "alt+up":
		state.nudge(1)
		return nil
	case "alt+down":
		state.nudge(-1)
		return nil
	case "left", "right":
		if token, ok := state.currentChosenNumber(); ok {
			state.numberChosen = false
			if key.String() == "left" {
				state.input.SetCursor(token.start)
			} else {
				state.input.SetCursor(token.end)
			}
			return nil
		}
	case "alt+left":
		state.selectNumber(-1)
		return nil
	case "alt+right":
		state.selectNumber(1)
		return nil
	case "home", "end", "ctrl+a", "ctrl+e", "ctrl+b", "ctrl+f", "ctrl+left", "ctrl+right":
		state.numberChosen = false
	}

	before := state.input.Value()
	if state.numberChosen {
		switch key.String() {
		case "backspace", "delete":
			state.removeChosenNumber()
			state.lines[state.active] = state.input.Value()
			state.refresh(true)
			return nil
		case "ctrl+v":
			state.removeChosenNumber()
		default:
			if key.Text != "" {
				state.removeChosenNumber()
			}
		}
	}
	updated, command := state.input.Update(key)
	state.input = updated
	if state.input.Value() != before {
		state.lines[state.active] = state.input.Value()
		state.selected = 0
		state.refresh(true)
	}
	return command
}

func (state *editor) startEditing() tea.Cmd {
	state.editing, state.selected = true, 0
	state.numberChosen = false
	state.input.SetValue(state.lines[state.active])
	state.input.CursorEnd()
	state.focusNearestNumber()
	state.resizeInput()
	state.refresh(false)
	return state.input.Focus()
}

func (state *editor) deleteActive() {
	if state.editing {
		state.editing = false
		state.input.Blur()
	}
	state.numberChosen = false
	if len(state.lines) == 1 {
		state.lines[0] = ""
	} else {
		state.lines = append(state.lines[:state.active], state.lines[state.active+1:]...)
		if state.active >= len(state.lines) {
			state.active = len(state.lines) - 1
		}
	}
	state.refresh(true)
}

func (state *editor) toggleMute() {
	state.muted = !state.muted
	if state.muted {
		state.live.close()
		state.message = "instrument muted"
		return
	}
	state.message = "instrument unmuted"
	if state.lastValid == nil {
		return
	}
	kind, err := state.live.apply(*state.lastValid)
	if err != nil {
		state.message = "audio: " + err.Error()
		return
	}
	state.message = string(kind)
}

func (state *editor) finishEditing() {
	state.lines[state.active] = state.input.Value()
	state.editing = false
	state.numberChosen = false
	state.input.Blur()
	state.refresh(true)
}

func (state *editor) selectSuggestion(direction int) {
	if len(state.suggestions) == 0 {
		return
	}
	state.selected = (state.selected + direction + len(state.suggestions)) % len(state.suggestions)
}

func (state *editor) insert(index int) tea.Cmd {
	if index < 0 {
		index = 0
	}
	if index > len(state.lines) {
		index = len(state.lines)
	}
	state.lines = append(state.lines, "")
	copy(state.lines[index+1:], state.lines[index:])
	state.lines[index] = ""
	state.active = index
	return state.startEditing()
}

func (state *editor) resizeInput() {
	contentWidth := state.width - 4
	width := contentWidth - 11
	if state.usesWideLayout() {
		leftWidth, _ := wideLayoutWidths(contentWidth)
		width = leftWidth - 11
	}
	if width < 12 {
		width = 12
	}
	state.input.SetWidth(width)
}

func (state *editor) refresh(apply bool) {
	state.analysis = Analyze(state.lines, state.config.Registry)
	if state.analysis.State == Valid {
		plan := state.analysis.Plan
		state.lastValid = &plan
	}
	state.suggestions = Complete(state.config.Registry, state.lines, state.active)
	if state.selected >= len(state.suggestions) {
		state.selected = 0
	}
	state.pollRuntime()
	if !apply || state.analysis.State != Valid || state.muted {
		return
	}
	kind, err := state.live.apply(state.analysis.Plan)
	if err != nil {
		state.message = "audio: " + err.Error()
		return
	}
	state.message = string(kind)
}

func (state *editor) pollRuntime() bool {
	runtimeErr := state.live.pollError()
	if runtimeErr == nil {
		return false
	}
	state.message = "audio stopped: " + runtimeErr.Error()
	if detail := state.backendLog.lastLine(); detail != "" {
		state.message += " (" + detail + ")"
	}
	return true
}

func (state *editor) View() tea.View {
	view := tea.NewView(state.render())
	view.AltScreen = true
	view.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	view.KeyboardEnhancements.ReportAssociatedText = true
	return view
}

func printableKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	modified := tea.ModCtrl | tea.ModAlt | tea.ModMeta | tea.ModHyper | tea.ModSuper
	if key.Text != "" || key.Mod&modified != 0 {
		return key
	}
	code := key.Code
	if key.Mod.Contains(tea.ModShift) && key.ShiftedCode != 0 {
		code = key.ShiftedCode
	}
	if code >= ' ' && code <= '~' {
		key.Text = string(code)
	}
	return key
}

func (state *editor) render() string {
	width := state.width
	if width <= 0 {
		width = 80
	}
	height := state.height
	if height <= 0 {
		height = 24
	}
	contentWidth := width - 4
	if contentWidth < 36 {
		contentWidth = 36
	}
	contentHeight := height - 2
	if contentHeight < 20 {
		contentHeight = 20
	}
	if state.presetMode != presetClosed {
		return state.presetView(width, height)
	}

	if state.usesWideLayout() {
		leftWidth, rightWidth := wideLayoutWidths(contentWidth)
		left := state.editorWorkspace(leftWidth, contentHeight)
		right := state.rail(rightWidth, contentHeight)
		main := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
		return lipgloss.NewStyle().Padding(1, 2).Render(main)
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		logoStyle.Render("STASH — live instrument"),
		"  ",
		subtitleStyle.Render("telemetry-driven sound"),
	)

	main := state.documentPanel(contentWidth, 0)
	if state.oscillator {
		main += "\n" + state.oscillatorPanel(contentWidth, min(12, max(8, contentHeight/2)))
	}
	main += "\n" + state.inspectorPanel(contentWidth, 0)

	status := state.statusView()
	footer := state.helpView(contentWidth)
	view := lipgloss.JoinVertical(lipgloss.Left, header, "", main, "", status, footer)
	return lipgloss.NewStyle().Padding(1, 2).Render(view)
}

func (state *editor) presetView(width, height int) string {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	dialogWidth := min(64, max(36, width-8))
	innerWidth := dialogWidth - 4
	var title, body, footer string
	if state.presetMode == presetSaving {
		title = "SAVE PRESET"
		body = mutedStyle.Render("Name this instrument") + "\n\n" +
			lipgloss.NewStyle().Background(surfaceColor).Padding(0, 1).Width(innerWidth).Render(state.presetInput.View())
		footer = helpKeyStyle.Render("enter") + helpTextStyle.Render(" save") +
			helpTextStyle.Render("   ") + helpKeyStyle.Render("esc") + helpTextStyle.Render(" cancel")
	} else {
		title = "LOAD PRESET"
		if len(state.presets) == 0 {
			body = mutedStyle.Render("No presets saved yet. Press Ctrl+G in the editor to create one.")
		} else {
			limit := min(10, max(3, height-12))
			start, end := suggestionWindow(state.presetSelected, len(state.presets), limit)
			rows := make([]string, 0, end-start+2)
			if start > 0 {
				rows = append(rows, mutedStyle.Render(fmt.Sprintf("  ↑ %d earlier", start)))
			}
			for index := start; index < end; index++ {
				prefix := "  "
				style := lipgloss.NewStyle()
				if index == state.presetSelected {
					prefix = "› "
					style = selectedStyle
				}
				rows = append(rows, style.MaxWidth(innerWidth).Render(prefix+state.presets[index].name))
			}
			if end < len(state.presets) {
				rows = append(rows, mutedStyle.Render(fmt.Sprintf("  ↓ %d more", len(state.presets)-end)))
			}
			body = strings.Join(rows, "\n")
		}
		footer = helpKeyStyle.Render("↑↓") + helpTextStyle.Render(" choose") +
			helpTextStyle.Render("   ") + helpKeyStyle.Render("enter") + helpTextStyle.Render(" load") +
			helpTextStyle.Render("   ") + helpKeyStyle.Render("esc") + helpTextStyle.Render(" cancel")
	}
	if state.message != "" {
		body += "\n\n" + errorDetailStyle.MaxWidth(innerWidth).Render(state.message)
	}
	dialog := panelStyle.Width(dialogWidth).Render(sectionStyle.Render(title) + "\n\n" + body + "\n\n" + footer)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, dialog)
}

func (state *editor) editorWorkspace(width, height int) string {
	if !state.oscillator {
		return state.documentPanel(width, height)
	}
	editorHeight := (height + 1) / 2
	oscillatorHeight := height - editorHeight - 1
	return lipgloss.JoinVertical(
		lipgloss.Left,
		state.documentPanel(width, editorHeight),
		"",
		state.oscillatorPanel(width, oscillatorHeight),
	)
}

func (state *editor) usesWideLayout() bool {
	return state.width >= 96 && state.height >= 24
}

func wideLayoutWidths(contentWidth int) (left, right int) {
	right = contentWidth * 28 / 100
	if right < 32 {
		right = 32
	}
	if right > 40 {
		right = 40
	}
	left = contentWidth - right - 2
	return left, right
}

func (state *editor) rail(width, height int) string {
	headerHeight := 2
	panelsHeight := height - headerHeight - 3
	suggestionsHeight := panelsHeight * 30 / 100
	if suggestionsHeight < 5 {
		suggestionsHeight = 5
	}
	if suggestionsHeight > 10 {
		suggestionsHeight = 10
	}
	infoHeight := panelsHeight * 40 / 100
	if infoHeight < 5 {
		infoHeight = 5
	}
	if infoHeight > 13 {
		infoHeight = 13
	}
	shortcutsHeight := panelsHeight - suggestionsHeight - infoHeight
	if shortcutsHeight < 7 {
		shortcutsHeight = 7
		infoHeight = panelsHeight - suggestionsHeight - shortcutsHeight
	}

	header := lipgloss.NewStyle().Width(width).Height(headerHeight).MaxHeight(headerHeight).Render(
		logoStyle.Render("STASH — live instrument") + "\n" +
			lipgloss.NewStyle().MaxWidth(width).Render(state.statusSummary()),
	)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		state.suggestionsPanel(width, suggestionsHeight),
		"",
		state.inspectorPanel(width, infoHeight),
		"",
		state.shortcutsPanel(width, shortcutsHeight),
	)
}

func (state *editor) documentPanel(width, height int) string {
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}
	start, end := state.visibleLines(height)
	rows := make([]string, 0, end-start+2)
	if start > 0 {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("  ↑ %d earlier clause(s)", start)))
	}
	for index := start; index < end; index++ {
		marker := "  "
		lineNumber := mutedStyle.Width(3).Align(lipgloss.Right).Render(strconv.Itoa(index + 1))
		line := state.lines[index]
		if index == state.active {
			marker = activeLineStyle.Render("◆ ")
			if state.editing {
				line = state.input.View()
				if state.numberChosen {
					line = state.numberSelectionView()
				}
			} else if line == "" {
				line = mutedStyle.Italic(true).Render("empty clause")
			} else {
				line = activeLineStyle.Render(highlightClause(line, index, state.lines, state.config.Registry))
			}
		} else if line == "" {
			line = mutedStyle.Italic(true).Render("empty clause")
		} else {
			line = highlightClause(line, index, state.lines, state.config.Registry)
		}
		rows = append(rows, lipgloss.NewStyle().MaxWidth(innerWidth).Render(marker+lineNumber+"  "+line))
	}
	if end < len(state.lines) {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("  ↓ %d later clause(s)", len(state.lines)-end)))
	}
	rowSeparator := "\n"
	if height > 0 {
		rowSeparator = "\n\n"
	}
	mode := "NAVIGATE"
	if state.editing {
		mode = "EDIT CLAUSE"
	}
	titleLeft := sectionStyle.Render("EDITOR") + mutedStyle.Render(fmt.Sprintf("  %d clauses", len(state.lines)))
	titleRight := modeStyle.Render(mode)
	titleGap := innerWidth - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if titleGap < 1 {
		titleGap = 1
	}
	title := titleLeft + strings.Repeat(" ", titleGap) + titleRight
	body := title + "\n\n" + strings.Join(rows, rowSeparator)
	style := panelStyle.Width(width)
	if height > 0 {
		style = style.Height(height)
	}
	return style.Render(body)
}

func (state *editor) visibleLines(panelHeight int) (int, int) {
	limit := state.height - 16
	if panelHeight > 0 {
		// The wide editor gives every clause a blank row of separation. Reserve
		// the border and heading, then turn the remaining height into rows.
		limit = (panelHeight - 7) / 2
	} else if state.width < 96 {
		limit = state.height - 17
		if state.editing {
			limit = state.height - 21
		}
	}
	if limit < 3 {
		limit = 3
	}
	if len(state.lines) <= limit {
		return 0, len(state.lines)
	}
	start := state.active - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > len(state.lines) {
		start = len(state.lines) - limit
	}
	return start, start + limit
}

func (state *editor) suggestionsPanel(width, height int) string {
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}
	var body strings.Builder
	if state.editing && len(state.suggestions) > 0 {
		limit := height - 6
		if limit < 1 {
			limit = 1
		}
		start, end := suggestionWindow(state.selected, len(state.suggestions), limit)
		for index := start; index < end; index++ {
			prefix := "  "
			style := lipgloss.NewStyle()
			if index == state.selected {
				prefix = "› "
				style = selectedStyle
			}
			label := highlightReference(state.suggestions[index].Label, state.lines, state.config.Registry)
			body.WriteString(style.MaxWidth(innerWidth).Render(prefix + label))
			body.WriteByte('\n')
		}
		if end < len(state.suggestions) {
			body.WriteString(mutedStyle.Render(fmt.Sprintf("  +%d more", len(state.suggestions)-end)))
			body.WriteByte('\n')
		}
		body.WriteByte('\n')
		body.WriteString(mutedStyle.MaxWidth(innerWidth).Render(compactHelp(state.suggestions[state.selected].Help, 1)))
	} else if state.editing {
		body.WriteString(mutedStyle.Width(innerWidth).Render("No completions for this clause yet."))
	} else {
		body.WriteString(mutedStyle.Width(innerWidth).Render("Edit a clause to see context-aware completions here."))
	}
	return railPanel("SUGGESTIONS", body.String(), width, height)
}

func (state *editor) inspectorPanel(width, height int) string {
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}
	body := ""
	if state.analysis.Err != nil {
		body = errorDetailStyle.Width(innerWidth).Render(state.analysis.Err.Error())
	} else {
		body = state.clauseInsight(innerWidth)
	}
	if detail := state.detailMessage(); detail != "" {
		body = labelStyle.Render("SESSION") + "\n" + mutedStyle.MaxWidth(innerWidth).Render(detail) + "\n\n" + body
	}
	if height <= 0 {
		return panelStyle.Width(width).Render(sectionStyle.Render("INFO") + "\n\n" + body)
	}
	return railPanel("INFO", body, width, height)
}

func railPanel(title, body string, width, height int) string {
	innerHeight := height - 3
	if innerHeight < 1 {
		innerHeight = 1
	}
	body = lipgloss.NewStyle().MaxHeight(innerHeight).Render(strings.TrimSpace(body))
	return panelStyle.Width(width).Height(height).Render(sectionStyle.Render(title) + "\n" + body)
}

func (state *editor) shortcutsPanel(width, height int) string {
	pairs := state.shortcutPairs()
	innerWidth := width - 4
	columns := 1
	if innerWidth >= 28 {
		columns = 2
	}
	columnWidth := innerWidth / columns
	rows := make([]string, 0, (len(pairs)+columns-1)/columns)
	for index := 0; index < len(pairs); index += columns {
		cells := make([]string, 0, columns)
		for column := 0; column < columns; column++ {
			pairIndex := index + column
			if pairIndex >= len(pairs) {
				cells = append(cells, strings.Repeat(" ", columnWidth))
				continue
			}
			pair := pairs[pairIndex]
			cell := helpKeyStyle.Render(pair[0]) + " " + helpTextStyle.Render(pair[1])
			cells = append(cells, lipgloss.NewStyle().Width(columnWidth).Render(cell))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return railPanel("SHORTCUTS", strings.Join(rows, "\n"), width, height)
}

func compactHelp(help string, limit int) string {
	lines := strings.Split(help, "\n")
	if len(lines) <= limit {
		return help
	}
	lines = lines[:limit]
	lines[limit-1] = strings.TrimSpace(lines[limit-1]) + " …"
	return strings.Join(lines, "\n")
}

func suggestionWindow(selected, count, limit int) (int, int) {
	if count <= limit {
		return 0, count
	}
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > count {
		start = count - limit
	}
	return start, start + limit
}

func (state *editor) statusView() string {
	status := state.statusSummary()
	if state.message != "" {
		status += mutedStyle.Render("  ·  ") + state.message
	}
	return status
}

func (state *editor) statusSummary() string {
	status := incompleteStyle.Render("… INCOMPLETE")
	if state.analysis.State == Valid {
		status = validStyle.Render("✓ VALID")
	} else if state.analysis.State == Invalid {
		status = invalidStyle.Render("✕ INVALID")
	}
	audioStatus := mutedStyle.Render("○ AUDIO IDLE")
	if state.muted {
		audioStatus = incompleteStyle.Render("○ AUDIO MUTED")
	} else if state.live.running() {
		audioStatus = validStyle.Render("● AUDIO LIVE")
	}
	status += mutedStyle.Render("  ·  ") + audioStatus
	return status
}

func (state *editor) detailMessage() string {
	switch state.message {
	case "", string(applyStarted), string(applyHot), string(applyRebuild), "instrument muted", "instrument unmuted":
		return ""
	default:
		return state.message
	}
}

func (state *editor) helpView(width int) string {
	pairs := state.shortcutPairs()
	if width < 60 {
		muteKey, deleteKey, presetsKey := "alt+m", "alt+d", "alt+g"
		if state.enhancedKeys {
			muteKey, deleteKey, presetsKey = "ctrl+m", "ctrl+shift+d", "ctrl+shift+g"
		}
		pairs = [][2]string{{"↑↓", "select"}, {"enter", "edit"}, {"ctrl+o", "osc"}, {"ctrl+g", "save"}, {presetsKey, "presets"}, {"q", "quit"}}
		if state.editing {
			pairs = [][2]string{{"alt+←→/↑↓", "select/nudge"}, {"ctrl+d", "done"}, {"ctrl+o", "osc"}, {deleteKey, "delete"}, {muteKey, "mute"}}
		}
	}
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, helpKeyStyle.Render(pair[0])+" "+helpTextStyle.Render(pair[1]))
	}
	return strings.Join(parts, helpTextStyle.Render("   "))
}

func (state *editor) shortcutPairs() [][2]string {
	muteKey, deleteKey, presetsKey := "alt+m", "alt+d", "alt+g"
	if state.enhancedKeys {
		muteKey, deleteKey, presetsKey = "ctrl+m", "ctrl+shift+d", "ctrl+shift+g"
	}
	pairs := [][2]string{
		{"↑↓", "select"}, {"enter", "edit"}, {"a", "add"}, {deleteKey, "delete"},
		{"ctrl+o", "oscillator"}, {muteKey, "mute"}, {"ctrl+g", "save preset"}, {presetsKey, "load preset"}, {"q", "quit"},
	}
	if state.editing {
		pairs = [][2]string{
			{"type", "change"}, {"tab/↑↓", "choose"}, {"enter", "accept"},
			{"alt+←→", "value"}, {"alt+↑↓", "nudge"}, {"ctrl+d", "done"},
			{"ctrl+o", "oscillator"}, {muteKey, "mute"},
		}
	}
	return pairs
}

func (state *editor) nudge(direction float64) {
	line := []rune(state.input.Value())
	start, end := 0, 0
	if token, ok := state.currentChosenNumber(); ok {
		start, end = token.start, token.end
	} else {
		start, end = numericToken(line, state.input.Position())
	}
	if start == end {
		start, end = nearestNumericToken(line, state.input.Position())
		if start == end {
			state.message = "this clause has no numeric value"
			return
		}
	}
	value, err := unit.ParseNumber(string(line[start:end]))
	if err != nil {
		state.message = "numeric value cannot be nudged"
		return
	}
	step := math.Max(math.Abs(value)*.01, .01)
	value += direction * step
	replacement := []rune(strconv.FormatFloat(value, 'g', 6, 64))
	line = append(line[:start], append(replacement, line[end:]...)...)
	state.input.SetValue(string(line))
	state.input.SetCursor(start + len(replacement))
	state.numberChosen = true
	state.chosenNumber = numericRange{start: start, end: start + len(replacement)}
	state.lines[state.active] = state.input.Value()
	state.refresh(true)
}

func numericToken(line []rune, cursor int) (int, int) {
	for _, token := range numericTokens(line) {
		if cursor >= token.start && cursor <= token.end {
			return token.start, token.end
		}
	}
	return 0, 0
}

type numericRange struct{ start, end int }

func numericTokens(line []rune) []numericRange {
	var result []numericRange
	for index := 0; index < len(line); {
		start := index
		if start > 0 && identifierRune(line[start-1]) {
			index++
			continue
		}
		if line[index] == '-' {
			index++
			if index >= len(line) {
				break
			}
		}
		digits := index
		for index < len(line) && line[index] >= '0' && line[index] <= '9' {
			index++
		}
		hasDigits := index > digits
		if index < len(line) && line[index] == '.' && index+1 < len(line) && line[index+1] >= '0' && line[index+1] <= '9' && leadingDecimal(line, index) {
			index++
			fraction := index
			for index < len(line) && line[index] >= '0' && line[index] <= '9' {
				index++
			}
			hasDigits = hasDigits || index > fraction
		}
		if !hasDigits {
			index = start + 1
			continue
		}
		if index < len(line) && (line[index] == 'k' || line[index] == 'M' || line[index] == 'G') {
			index++
		}
		if (index < len(line) && identifierRune(line[index]) && !durationSuffix(line[index:])) ||
			(index+1 < len(line) && line[index] == '.' && identifierRune(line[index+1])) {
			index = start + 1
			continue
		}
		result = append(result, numericRange{start: start, end: index})
	}
	return result
}

func leadingDecimal(line []rune, dot int) bool {
	count := 1
	for index := dot - 1; index >= 0 && line[index] == '.'; index-- {
		count++
	}
	return count%2 == 1
}

func identifierRune(value rune) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || value == '_'
}

func durationSuffix(suffix []rune) bool {
	return suffixBoundary(suffix, "s") || suffixBoundary(suffix, "ms")
}

func suffixBoundary(suffix []rune, want string) bool {
	value := []rune(want)
	if len(suffix) < len(value) || string(suffix[:len(value)]) != want {
		return false
	}
	return len(suffix) == len(value) || !identifierRune(suffix[len(value)])
}

func nearestNumericToken(line []rune, cursor int) (int, int) {
	tokens := numericTokens(line)
	if len(tokens) == 0 {
		return 0, 0
	}
	best, distance := tokens[0], len(line)+1
	for _, token := range tokens {
		current := 0
		if cursor < token.start {
			current = token.start - cursor
		} else if cursor > token.end {
			current = cursor - token.end
		}
		if current < distance {
			best, distance = token, current
		}
	}
	return best.start, best.end
}

func (state *editor) focusNearestNumber() bool {
	start, end := nearestNumericToken([]rune(state.input.Value()), state.input.Position())
	if start == end {
		state.numberChosen = false
		return false
	}
	state.input.SetCursor(end)
	state.numberChosen = true
	state.chosenNumber = numericRange{start: start, end: end}
	return true
}

func (state *editor) selectNumber(direction int) {
	tokens := numericTokens([]rune(state.input.Value()))
	if len(tokens) == 0 {
		state.numberChosen = false
		state.message = "this clause has no numeric value"
		return
	}
	cursor, selected := state.input.Position(), -1
	for index, token := range tokens {
		if cursor >= token.start && cursor <= token.end {
			selected = index
			break
		}
	}
	if selected < 0 {
		if direction < 0 {
			selected = len(tokens)
		} else {
			selected = -1
		}
	}
	selected = (selected + direction + len(tokens)) % len(tokens)
	state.input.SetCursor(tokens[selected].end)
	state.numberChosen = true
	state.chosenNumber = tokens[selected]
}

func (state *editor) currentChosenNumber() (numericRange, bool) {
	if !state.numberChosen {
		return numericRange{}, false
	}
	for _, token := range numericTokens([]rune(state.input.Value())) {
		if token == state.chosenNumber {
			return token, true
		}
	}
	state.numberChosen = false
	return numericRange{}, false
}

func (state *editor) removeChosenNumber() bool {
	token, ok := state.currentChosenNumber()
	if !ok {
		return false
	}
	line := []rune(state.input.Value())
	line = append(line[:token.start], line[token.end:]...)
	state.input.SetValue(string(line))
	state.input.SetCursor(token.start)
	state.numberChosen = false
	return true
}

func (state *editor) numberSelectionView() string {
	token, ok := state.currentChosenNumber()
	if !ok {
		return state.input.View()
	}
	line := []rune(state.input.Value())
	first, last := 0, len(line)
	if width := state.input.Width(); width > 0 && last > width {
		last = token.end
		first = last - width
		if first > token.start {
			first = token.start
		}
		if first < 0 {
			first = 0
			last = min(len(line), width)
		}
	}
	style := state.input.Styles().Focused.Text.Inline(true)
	return style.Render(string(line[first:token.start])) +
		numericSelectionStyle.Render(string(line[token.start:token.end])) +
		style.Render(string(line[token.end:last]))
}

type diagnosticCapture struct {
	mu   sync.Mutex
	data []byte
}

func (capture *diagnosticCapture) Write(value []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.data = append(capture.data, value...)
	if len(capture.data) > 16*1024 {
		capture.data = append([]byte(nil), capture.data[len(capture.data)-16*1024:]...)
	}
	return len(value), nil
}

func (capture *diagnosticCapture) lastLine() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	lines := strings.Split(stripANSI(string(capture.data)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.Join(strings.Fields(lines[index]), " "); line != "" {
			return line
		}
	}
	return ""
}

func stripANSI(input string) string {
	var result strings.Builder
	state := uint8(0)
	for _, value := range input {
		switch state {
		case 0:
			if value == '\x1b' {
				state = 1
				continue
			}
			result.WriteRune(value)
		case 1:
			if value == '[' {
				state = 2
			} else {
				state = 0
			}
		case 2:
			if value >= '@' && value <= '~' {
				state = 0
			}
		}
	}
	return result.String()
}
