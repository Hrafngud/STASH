package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zalmo/stash/internal/sound"
)

func TestControlOTogglesOscillatorBelowSplitEditor(t *testing.T) {
	state := newTestEditor(t)
	_, _ = state.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if strings.Contains(state.View().Content, "OSCILLATOR") {
		t.Fatal("oscillator is visible before Ctrl+O")
	}

	_, _ = state.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !state.oscillator {
		t.Fatal("Ctrl+O did not enable the oscillator")
	}
	view := state.View().Content
	if !strings.Contains(view, "OSCILLATOR") || !strings.Contains(view, "FM:BASS / SINE") {
		t.Fatalf("oscillator panel is missing its signal identity:\n%s", view)
	}
	if lipgloss.Width(view) != 120 || lipgloss.Height(view) != 32 {
		t.Fatalf("split view = %dx%d, want 120x32", lipgloss.Width(view), lipgloss.Height(view))
	}
	leftWidth, _ := wideLayoutWidths(116)
	workspace := state.editorWorkspace(leftWidth, 30)
	if lipgloss.Height(workspace) != 30 || strings.Index(workspace, "EDITOR") > strings.Index(workspace, "OSCILLATOR") {
		t.Fatal("oscillator is not directly below the editor in the split workspace")
	}

	state.startEditing()
	_, _ = state.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if state.oscillator {
		t.Fatal("Ctrl+O did not disable the oscillator from edit mode")
	}
}

func TestOscillatorShortcutIsVisibleInCompactHelp(t *testing.T) {
	state := newTestEditor(t)
	if !strings.Contains(state.helpView(50), "ctrl+o") {
		t.Fatal("compact help does not advertise the oscillator shortcut")
	}
}

func TestOscillatorDotsAnimateUsingOnlyASCIIDots(t *testing.T) {
	signal := oscillatorSignal{wave: sound.WaveSine, frequency: 440, gain: .4, label: "VOICE / SINE"}
	first := oscillatorDots(signal, 48, 7, 0, false)
	second := oscillatorDots(signal, 48, 7, 1, false)
	if first == second {
		t.Fatal("oscillator did not advance with its frame")
	}
	if strings.Count(first, ".") != 48 {
		t.Fatalf("oscillator dots = %d, want one per column", strings.Count(first, "."))
	}
	for _, value := range first {
		if value != ' ' && value != '.' && value != '\n' {
			t.Fatalf("oscillator contains non-ASCII plot character %q", value)
		}
	}
}

func TestOscillatorFrameAdvancesOnlyWhenVisibleAndUnmuted(t *testing.T) {
	state := newTestEditor(t)
	_, _ = state.Update(runtimeTick(time.Now()))
	if state.oscillatorFrame != 0 {
		t.Fatalf("hidden oscillator frame = %d, want 0", state.oscillatorFrame)
	}
	state.oscillator = true
	_, _ = state.Update(runtimeTick(time.Now()))
	if state.oscillatorFrame != 1 {
		t.Fatalf("visible oscillator frame = %d, want 1", state.oscillatorFrame)
	}
	state.muted = true
	_, _ = state.Update(runtimeTick(time.Now()))
	if state.oscillatorFrame != 1 {
		t.Fatalf("muted oscillator frame = %d, want 1", state.oscillatorFrame)
	}
}
