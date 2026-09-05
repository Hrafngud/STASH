package tui

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if state.width != 120 || state.height != 32 || state.input.Width() <= 12 {
		t.Fatalf("resized editor = %dx%d, input width %d", state.width, state.height, state.input.Width())
	}

	_, _ = state.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !state.editing || !state.input.Focused() {
		t.Fatal("Enter did not focus the clause input")
	}
	_, _ = state.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got, want := state.lines[0], "cpu.usagex"; got != want {
		t.Fatalf("edited clause = %q, want %q", got, want)
	}
	_, _ = state.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !state.editing || !state.input.Focused() {
		t.Fatal("Escape unexpectedly left edit mode")
	}
	_, _ = state.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if state.editing || state.input.Focused() {
		t.Fatal("Ctrl+D did not return to navigation mode")
	}

	view := state.View().Content
	for _, want := range []string{"STASH — live instrument", "INSTRUMENT", "INSPECTOR", "✕", "AUDIO IDLE"} {
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

func TestEditorFindsAndCyclesNumericValuesForNudging(t *testing.T) {
	state := newTestEditor(t)
	state.lines[1] = "-s fm:bass,ratio=2,index=4"
	state.active = 1
	state.startEditing()
	state.input.CursorStart()
	state.numberChosen = false
	state.nudge(1)
	if got := state.lines[1]; !strings.Contains(got, "ratio=2.02") {
		t.Fatalf("automatically focused clause = %q", got)
	}

	_, _ = state.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	state.nudge(1)
	if got := state.lines[1]; !strings.Contains(got, "index=4.04") {
		t.Fatalf("cycled numeric clause = %q", got)
	}
}

func TestArrowKeysLeaveNumericSelectionForTextEditing(t *testing.T) {
	for _, test := range []struct {
		name   string
		key    tea.KeyPressMsg
		cursor int
		insert string
		want   string
	}{
		{name: "left", key: tea.KeyPressMsg{Code: tea.KeyLeft}, cursor: len([]rune("-s fm:bass,index=")), insert: "x", want: "-s fm:bass,index=x4,gain=.2"},
		{name: "right", key: tea.KeyPressMsg{Code: tea.KeyRight}, cursor: len([]rune("-s fm:bass,index=4")), insert: "x", want: "-s fm:bass,index=4x,gain=.2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newTestEditor(t)
			state.lines[1] = "-s fm:bass,index=4,gain=.2"
			state.active = 1
			state.startEditing()
			state.input.SetCursor(len([]rune("-s fm:bass,index=4")))
			state.focusNearestNumber()

			_, _ = state.Update(test.key)
			if state.numberChosen || state.input.Position() != test.cursor {
				t.Fatalf("selection left chosen=%t at cursor %d, want cursor %d", state.numberChosen, state.input.Position(), test.cursor)
			}
			_, _ = state.Update(tea.KeyPressMsg{Code: 'x', Text: test.insert})
			if got := state.lines[1]; got != test.want {
				t.Fatalf("edited clause = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNumericTokensHandleRangesDurationsAndIdentifiers(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"-m freq=45..90", []string{"45", "90"}},
		{"-m syn.fm1.out:syn.add1.freq.mod=-.4...4", []string{"-.4", ".4"}},
		{"-a 5ms,20ms,0.8,50ms", []string{"5", "20", "0.8", "50"}},
		{"-m syn.add.partial.1.gain=0.5", []string{"0.5"}},
	}
	for _, test := range tests {
		line := []rune(test.line)
		tokens := numericTokens(line)
		got := make([]string, len(tokens))
		for index, token := range tokens {
			got[index] = string(line[token.start:token.end])
		}
		if strings.Join(got, ",") != strings.Join(test.want, ",") {
			t.Errorf("numericTokens(%q) = %v, want %v", test.line, got, test.want)
		}
	}
}

func TestEditorControlShortcutsDeleteAndMute(t *testing.T) {
	state := newTestEditor(t)
	state.active = 1
	state.startEditing()
	_, _ = state.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModCtrl})
	if !state.muted {
		t.Fatal("Ctrl+M did not mute the instrument")
	}
	_, _ = state.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModCtrl})
	if state.muted {
		t.Fatal("Ctrl+M did not unmute the instrument")
	}

	_, _ = state.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl | tea.ModShift})
	if state.editing || len(state.lines) != 1 {
		t.Fatalf("Ctrl+Shift+D left editing=%t, lines=%v", state.editing, state.lines)
	}
}

func TestEditorLegacyTerminalFallbackShortcuts(t *testing.T) {
	state := newTestEditor(t)
	_, _ = state.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	if !state.muted {
		t.Fatal("Alt+M did not mute without enhanced keyboard support")
	}

	_, _ = state.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt})
	if len(state.lines) != 1 {
		t.Fatalf("Alt+D left lines=%v", state.lines)
	}
	if !strings.Contains(state.helpView(80), "alt+m") {
		t.Fatal("fallback shortcut is missing from help")
	}

	_, _ = state.Update(tea.KeyboardEnhancementsMsg{Flags: 1})
	if !state.enhancedKeys || !strings.Contains(state.helpView(80), "ctrl+m") {
		t.Fatal("enhanced shortcut is missing from help")
	}
	view := state.View()
	if !view.KeyboardEnhancements.ReportAllKeysAsEscapeCodes || !view.KeyboardEnhancements.ReportAssociatedText {
		t.Fatal("view does not request enhanced keys with their associated text")
	}
}

func TestTypingReplacesSelectedNumericDefault(t *testing.T) {
	state := newTestEditor(t)
	state.lines[1] = "-s fm:bass,i"
	state.active = 1
	state.startEditing()

	_, _ = state.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	token, ok := state.currentChosenNumber()
	if !ok || string([]rune(state.input.Value())[token.start:token.end]) != "1" {
		t.Fatalf("accepted completion did not select its default: %q, %#v", state.input.Value(), token)
	}
	_, _ = state.Update(tea.KeyPressMsg{Code: '4'})
	if got, want := state.lines[1], "-s fm:bass,index=4"; got != want {
		t.Fatalf("typed replacement = %q, want %q", got, want)
	}
	if state.analysis.State != Valid {
		t.Fatalf("typed replacement is invalid: %v", state.analysis.Err)
	}
}

func TestPasteReplacesWholeSelectedNumber(t *testing.T) {
	state := newTestEditor(t)
	state.active = 1
	state.startEditing()
	_, _ = state.Update(tea.PasteMsg{Content: "12"})
	if got := state.lines[1]; !strings.Contains(got, "index=12") || strings.Contains(got, "index=412") {
		t.Fatalf("pasted numeric replacement = %q", got)
	}
}

func TestEditorImportsTerminalCommandPaste(t *testing.T) {
	command := "stash cpu.usage \\\n" +
		"  -b 154 \\\n" +
		"  -r rhythm:1/32:x--x-x-x--xx-x-- \\\n" +
		"  -s fm:test,wave=saw,ratio=2,index=2,gain=.22 \\\n" +
		"  -m syn.test.freq=90..420/exp~120ms \\\n" +
		"  -v .9"
	want := []string{
		"cpu.usage",
		"-b 154",
		"-r rhythm:1/32:x--x-x-x--xx-x--",
		"-s fm:test,wave=saw,ratio=2,index=2,gain=.22",
		"-m syn.test.freq=90..420/exp~120ms",
		"-v .9",
	}
	for _, editing := range []bool{false, true} {
		state := newTestEditor(t)
		if editing {
			state.startEditing()
		}
		_, _ = state.Update(tea.PasteMsg{Content: command})
		if !reflect.DeepEqual(state.lines, want) {
			t.Fatalf("editing=%t: pasted lines = %#v, want %#v", editing, state.lines, want)
		}
		if state.editing || state.active != 0 {
			t.Fatalf("editing=%t: editor remained editing=%t at line %d", editing, state.editing, state.active)
		}
		if state.analysis.State != Valid {
			t.Fatalf("editing=%t: imported command is not valid: %v", editing, state.analysis.Err)
		}
	}
}

func TestNumericSelectionViewHighlightsWholeValue(t *testing.T) {
	state := newTestEditor(t)
	state.lines[1] = "-s fm:bass,index=400"
	state.active = 1
	state.startEditing()

	token, ok := state.currentChosenNumber()
	if !ok {
		t.Fatal("numeric value was not selected")
	}
	if got := string([]rune(state.input.Value())[token.start:token.end]); got != "400" {
		t.Fatalf("selected numeric value = %q, want %q", got, "400")
	}
	if got, want := state.numberSelectionView(), numericSelectionStyle.Render("400"); !strings.Contains(got, want) {
		t.Fatalf("selection view does not highlight the whole numeric value: %q", got)
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
	if !strings.Contains(output.String(), "\x1b[?1049l") {
		t.Fatalf("Bubble Tea did not restore the primary screen: %q", output.String())
	}
}

func TestSuggestionWindowKeepsSelectionVisible(t *testing.T) {
	start, end := suggestionWindow(8, 12, 6)
	if start > 8 || end <= 8 || end-start != 6 {
		t.Fatalf("suggestion window = %d..%d", start, end)
	}
}
