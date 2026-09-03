package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zalmo/stash/internal/audio"
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
	SampleInterval func(string) time.Duration
	RhythmInterval time.Duration
	MaxDelay       time.Duration
}

type editor struct {
	config      Config
	lines       []string
	active      int
	editing     bool
	input       textinput.Model
	suggestions []Suggestion
	selected    int
	analysis    Analysis
	message     string
	width       int
	height      int
	exported    bool
	live        *liveEngine
	backendLog  *diagnosticCapture
}

type runtimeTick time.Time

const runtimePollInterval = 100 * time.Millisecond

var (
	accentColor  = lipgloss.AdaptiveColor{Light: "#5B21B6", Dark: "#C4A7E7"}
	cyanColor    = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#7DD3FC"}
	greenColor   = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"}
	yellowColor  = lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FDE68A"}
	redColor     = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"}
	mutedColor   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#8B93A7"}
	borderColor  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#414559"}
	surfaceColor = lipgloss.AdaptiveColor{Light: "#F3F4F6", Dark: "#24273A"}

	logoStyle        = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	subtitleStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	sectionStyle     = lipgloss.NewStyle().Bold(true).Foreground(cyanColor)
	panelStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderColor).Padding(0, 1)
	activeLineStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	mutedStyle       = lipgloss.NewStyle().Foreground(mutedColor)
	selectedStyle    = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Background(surfaceColor)
	helpKeyStyle     = lipgloss.NewStyle().Bold(true).Foreground(cyanColor)
	helpTextStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	validStyle       = lipgloss.NewStyle().Bold(true).Foreground(greenColor)
	incompleteStyle  = lipgloss.NewStyle().Bold(true).Foreground(yellowColor)
	invalidStyle     = lipgloss.NewStyle().Bold(true).Foreground(redColor)
	modeStyle        = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Background(surfaceColor).Padding(0, 1)
	errorDetailStyle = lipgloss.NewStyle().Foreground(redColor)
)

// Run opens the Bubble Tea instrument editor. Ctrl-G exports and exits; q
// exits without exporting. The returned command is written after Bubble Tea
// has restored the terminal by the caller.
func Run(ctx context.Context, config Config) (export string, exported bool, err error) {
	if ctx == nil {
		return "", false, fmt.Errorf("open TUI: context is nil")
	}
	if config.Registry == nil || config.Input == nil || config.Output == nil || config.Diagnostics == nil {
		return "", false, fmt.Errorf("open TUI: registry, input, output, and diagnostics are required")
	}

	state := newEditor(ctx, config)
	defer state.live.close()
	program := tea.NewProgram(
		state,
		tea.WithContext(ctx),
		tea.WithInput(config.Input),
		tea.WithOutput(config.Output),
		tea.WithAltScreen(),
		tea.WithoutSignalHandler(),
	)
	finalModel, runErr := program.Run()
	if runErr != nil {
		if errors.Is(runErr, tea.ErrProgramKilled) && ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		if errors.Is(runErr, tea.ErrInterrupted) {
			return "", false, nil
		}
		return "", false, runErr
	}
	final, ok := finalModel.(*editor)
	if !ok || !final.exported {
		return "", false, nil
	}
	return Command(final.lines), true, nil
}

func newEditor(ctx context.Context, config Config) *editor {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "type a source or option"
	input.CharLimit = 0
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#111827", Dark: "#F4F4F5"})
	input.PlaceholderStyle = mutedStyle.Italic(true)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(accentColor)

	state := &editor{
		config: config, lines: initialLines(config.Registry), input: input,
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
		return state, pollRuntimeCmd()
	case tea.KeyMsg:
		return state.updateKey(message)
	}
	if state.editing {
		before := state.input.Value()
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

func (state *editor) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state.message = ""
	name := key.String()
	if name == "ctrl+c" || !state.editing && (name == "q" || name == "esc") {
		return state, tea.Quit
	}
	if name == "ctrl+g" {
		if state.analysis.State != Valid {
			state.message = "finish the instrument before exporting"
			return state, nil
		}
		state.exported = true
		return state, tea.Quit
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
	case "d":
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
	return nil
}

func (state *editor) updateEdit(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		state.finishEditing()
		return nil
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
	}

	before := state.input.Value()
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
	state.input.SetValue(state.lines[state.active])
	state.input.CursorEnd()
	state.resizeInput()
	state.refresh(false)
	return state.input.Focus()
}

func (state *editor) finishEditing() {
	state.lines[state.active] = state.input.Value()
	state.editing = false
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
	width := state.width - 14
	if state.width >= 96 {
		width = state.width*3/5 - 14
	}
	if width < 12 {
		width = 12
	}
	state.input.Width = width
}

func (state *editor) refresh(apply bool) {
	state.analysis = Analyze(state.lines, state.config.Registry)
	state.suggestions = Complete(state.config.Registry, state.lines, state.active)
	if state.selected >= len(state.suggestions) {
		state.selected = 0
	}
	state.pollRuntime()
	if !apply || state.analysis.State != Valid {
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

func (state *editor) View() string {
	width := state.width
	if width <= 0 {
		width = 80
	}
	contentWidth := width - 4
	if contentWidth < 36 {
		contentWidth = 36
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		logoStyle.Render("STASH — live instrument"),
		"  ",
		subtitleStyle.Render("telemetry-driven sound"),
	)
	mode := "NAVIGATE"
	if state.editing {
		mode = "EDIT CLAUSE"
	}
	headerGap := contentWidth - lipgloss.Width(header) - lipgloss.Width(modeStyle.Render(mode))
	if headerGap < 1 {
		headerGap = 1
	}
	header += strings.Repeat(" ", headerGap) + modeStyle.Render(mode)

	main := state.documentPanel(contentWidth)
	if contentWidth >= 92 {
		leftWidth := contentWidth * 3 / 5
		rightWidth := contentWidth - leftWidth - 2
		main = lipgloss.JoinHorizontal(lipgloss.Top, state.documentPanel(leftWidth), "  ", state.inspectorPanel(rightWidth))
	} else {
		main += "\n" + state.inspectorPanel(contentWidth)
	}

	status := state.statusView()
	footer := state.helpView(contentWidth)
	view := lipgloss.JoinVertical(lipgloss.Left, header, "", main, "", status, footer)
	return lipgloss.NewStyle().Padding(1, 2).Render(view)
}

func (state *editor) documentPanel(width int) string {
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}
	start, end := state.visibleLines()
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
			} else if line == "" {
				line = mutedStyle.Italic(true).Render("empty clause")
			} else {
				line = activeLineStyle.Render(line)
			}
		} else if line == "" {
			line = mutedStyle.Italic(true).Render("empty clause")
		}
		rows = append(rows, lipgloss.NewStyle().MaxWidth(innerWidth).Render(marker+lineNumber+"  "+line))
	}
	if end < len(state.lines) {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("  ↓ %d later clause(s)", len(state.lines)-end)))
	}
	title := sectionStyle.Render("INSTRUMENT") + mutedStyle.Render(fmt.Sprintf("  %d clauses", len(state.lines)))
	body := title + "\n\n" + strings.Join(rows, "\n")
	return panelStyle.Width(innerWidth).Render(body)
}

func (state *editor) visibleLines() (int, int) {
	limit := state.height - 16
	if state.width < 96 {
		limit = state.height - 17
		if state.editing {
			limit = state.height - 21
		}
	}
	if limit < 3 {
		limit = 3
	}
	if limit > 12 {
		limit = 12
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

func (state *editor) inspectorPanel(width int) string {
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}
	title := sectionStyle.Render("COMPLETIONS")
	var body strings.Builder
	body.WriteString(title)
	body.WriteString("\n\n")
	if state.editing && len(state.suggestions) > 0 {
		limit, helpLines := 6, 4
		if state.width < 96 {
			limit, helpLines = 2, 1
		}
		start, end := suggestionWindow(state.selected, len(state.suggestions), limit)
		for index := start; index < end; index++ {
			prefix := "  "
			style := lipgloss.NewStyle()
			if index == state.selected {
				prefix = "› "
				style = selectedStyle
			}
			body.WriteString(style.MaxWidth(innerWidth).Render(prefix + state.suggestions[index].Label))
			body.WriteByte('\n')
		}
		if end < len(state.suggestions) {
			body.WriteString(mutedStyle.Render(fmt.Sprintf("  +%d more", len(state.suggestions)-end)))
			body.WriteByte('\n')
		}
		body.WriteByte('\n')
		body.WriteString(mutedStyle.MaxWidth(innerWidth).Render(compactHelp(state.suggestions[state.selected].Help, helpLines)))
	} else if state.analysis.Err != nil {
		body.WriteString(errorDetailStyle.Width(innerWidth).Render(state.analysis.Err.Error()))
	} else {
		body.WriteString(mutedStyle.Italic(true).Render("Enter edit mode to see contextual choices."))
	}
	return panelStyle.Width(innerWidth).Render(strings.TrimRight(body.String(), "\n"))
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
	status := incompleteStyle.Render("● INCOMPLETE")
	if state.analysis.State == Valid {
		status = validStyle.Render("● VALID")
	} else if state.analysis.State == Invalid {
		status = invalidStyle.Render("● INVALID")
	}
	audioStatus := mutedStyle.Render("○ AUDIO IDLE")
	if state.live.running() {
		audioStatus = validStyle.Render("● AUDIO LIVE")
	}
	status += mutedStyle.Render("  ·  ") + audioStatus
	if state.message != "" {
		status += mutedStyle.Render("  ·  ") + state.message
	}
	return status
}

func (state *editor) helpView(width int) string {
	pairs := [][2]string{
		{"↑↓", "select"}, {"enter", "edit"}, {"a", "add"}, {"d", "delete"},
		{"ctrl+↑↓", "move"}, {"ctrl+g", "export"}, {"q", "quit"},
	}
	if state.editing {
		pairs = [][2]string{
			{"type", "change"}, {"tab/↑↓", "choose"}, {"enter", "accept"},
			{"alt+↑↓", "nudge"}, {"ctrl+n/p", "add"}, {"esc", "done"},
		}
	}
	if width < 60 {
		pairs = [][2]string{{"↑↓", "select"}, {"enter", "edit"}, {"ctrl+g", "export"}, {"q", "quit"}}
		if state.editing {
			pairs = [][2]string{{"type", "change"}, {"tab", "choose"}, {"enter", "accept"}, {"esc", "done"}}
		}
	}
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, helpKeyStyle.Render(pair[0])+" "+helpTextStyle.Render(pair[1]))
	}
	return strings.Join(parts, helpTextStyle.Render("   "))
}

func (state *editor) nudge(direction float64) {
	line := []rune(state.input.Value())
	start, end := numericToken(line, state.input.Position())
	if start == end {
		state.message = "place the cursor on a numeric value"
		return
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
	state.lines[state.active] = state.input.Value()
	state.refresh(true)
}

func numericToken(line []rune, cursor int) (int, int) {
	if cursor > len(line) {
		cursor = len(line)
	}
	inside := func(value rune) bool {
		return value >= '0' && value <= '9' || value == '.' || value == '-' || value == '+' || value == 'k' || value == 'M' || value == 'G'
	}
	start := cursor
	if start == len(line) || start < len(line) && !inside(line[start]) {
		start--
	}
	if start < 0 || !inside(line[start]) {
		return 0, 0
	}
	end := start + 1
	for start > 0 && inside(line[start-1]) {
		start--
	}
	for end < len(line) && inside(line[end]) {
		end++
	}
	return start, end
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
