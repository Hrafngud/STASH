package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestEditor(t *testing.T) *editor {
	t.Helper()
	state := newEditor(context.Background(), Config{
		Registry: testRegistry(t), Input: strings.NewReader(""),
		Output: io.Discard, Diagnostics: io.Discard,
	})
	t.Cleanup(state.live.close)
	return state
}

func TestEditorUsesBubbleTeaResizeAndEditMessages(t *testing.T) {
	state := newTestEditor(t)
	_, _ = state.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if state.width != 120 || state.height != 32 || state.input.Width <= 12 {
		t.Fatalf("resized editor = %dx%d, input width %d", state.width, state.height, state.input.Width)
	}

	_, _ = state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !state.editing || !state.input.Focused() {
		t.Fatal("Enter did not focus the clause input")
	}
	_, _ = state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got, want := state.lines[0], "cpu.usagex"; got != want {
		t.Fatalf("edited clause = %q, want %q", got, want)
	}
	_, _ = state.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if state.editing || state.input.Focused() {
		t.Fatal("Escape did not return to navigation mode")
	}

	view := state.View()
	for _, want := range []string{"STASH — live instrument", "INSTRUMENT", "COMPLETIONS", "INVALID", "AUDIO IDLE"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain %q", want)
		}
	}
}

func TestEditorNudgesNumberAtUnicodeAwareCursor(t *testing.T) {
	state := newTestEditor(t)
	state.active = 1
	state.startEditing()
	state.nudge(1)
	if got := state.lines[1]; !strings.Contains(got, "index=4.04") {
		t.Fatalf("nudged clause = %q", got)
	}
}

func TestRunExportsAfterBubbleTeaRestoresScreen(t *testing.T) {
	var output bytes.Buffer
	command, exported, err := Run(context.Background(), Config{
		Registry: testRegistry(t), Input: strings.NewReader("\a"),
		Output: &output, Diagnostics: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !exported || !strings.HasPrefix(command, "stash cpu.usage") {
		t.Fatalf("export = %q, %t", command, exported)
	}
	if !strings.Contains(output.String(), "STASH — live instrument") {
		t.Fatal("Bubble Tea did not render the editor")
	}
}

func TestSuggestionWindowKeepsSelectionVisible(t *testing.T) {
	start, end := suggestionWindow(8, 12, 6)
	if start > 8 || end <= 8 || end-start != 6 {
		t.Fatalf("suggestion window = %d..%d", start, end)
	}
}
