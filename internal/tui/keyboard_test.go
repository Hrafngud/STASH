package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Feed the wire encoding through Bubble Tea's real input reader, rather than
// assuming a particular KeyPressMsg shape. q exits after the action under test.
func runKeyboardSequence(t *testing.T, state *editor, sequence string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	program := tea.NewProgram(state,
		tea.WithContext(ctx),
		tea.WithInput(strings.NewReader(sequence)), tea.WithOutput(io.Discard),
		tea.WithoutSignalHandler(),
	)
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalEncodedMuteAndDelete(t *testing.T) {
	for _, test := range []struct {
		name     string
		sequence string
		mute     bool
	}{
		{"legacy alt m", "\x1bm", true},
		{"kitty ctrl m", "\x1b[109;5u", true},
		{"kitty ctrl m with text", "\x1b[109;5;109u", true},
		{"kitty ctrl m with caps lock", "\x1b[109;69u", true},
		{"legacy alt d", "\x1bd", false},
		{"kitty alt d with text", "\x1b[100;3;100u", false},
		{"kitty ctrl shift d", "\x1b[100;6u", false},
		{"kitty ctrl shift d with alternate and text", "\x1b[100:68;6;68u", false},
		{"kitty ctrl shift d with num lock", "\x1b[100;134u", false},
		{"xterm ctrl shift d", "\x1b[27;6;68~", false},
	} {
		for _, editing := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/navigation", true: "/editing"}[editing], func(t *testing.T) {
				state := newTestEditor(t)
				state.active = 1
				initial := len(state.lines)
				if editing {
					state.startEditing()
				}
				// Ctrl+D completes editing for mute tests; deletion already
				// leaves editing, and Ctrl+D has no navigation binding.
				runKeyboardSequence(t, state, test.sequence+"\x04q")
				if test.mute {
					if !state.muted || len(state.lines) != initial {
						t.Fatalf("mute: muted=%t, clauses=%v", state.muted, state.lines)
					}
				} else if len(state.lines) != initial-1 || state.editing {
					t.Fatalf("delete: editing=%t, clauses=%v", state.editing, state.lines)
				}
			})
		}
	}
}

func TestTerminalRepeatDoesNotRetoggleOrDeleteAnotherClause(t *testing.T) {
	for _, test := range []struct {
		name     string
		sequence string
		mute     bool
	}{
		{"mute", "\x1b[109;5:1u\x1b[109;5:2u\x1b[109;5:2u\x1b[109;5:3u", true},
		{"delete", "\x1b[100;6:1u\x1b[100;6:2u\x1b[100;6:2u\x1b[100;6:3u", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newTestEditor(t)
			initial := len(state.lines)
			runKeyboardSequence(t, state, test.sequence+"q")
			if test.mute {
				if !state.muted || len(state.lines) != initial {
					t.Fatalf("repeated mute: muted=%t, clauses=%v", state.muted, state.lines)
				}
			} else if len(state.lines) != initial-1 {
				t.Fatalf("repeated delete: clauses=%v", state.lines)
			}
		})
	}
}

func TestLegacyEnterIsNeverInterpretedAsMute(t *testing.T) {
	state := newTestEditor(t)
	// Legacy Ctrl+M and Enter share this byte. Only enhanced encoding can
	// distinguish them; guessing would steal the user's Enter key.
	runKeyboardSequence(t, state, "\r\x04q")
	if state.muted || state.input.Value() != "cpu.usage" {
		t.Fatalf("legacy Enter: muted=%t, input=%q", state.muted, state.input.Value())
	}
}

func TestTerminalTextRemainsIndependentOfShortcutCodes(t *testing.T) {
	state := newTestEditor(t)
	state.lines[0] = ""
	state.startEditing()
	// Unicode, a shifted punctuation key, and a text-only IME event.
	runKeyboardSequence(t, state, "\x1b[233;1;233u\x1b[47:63;2;63u\x1b[0;1;101:769u\x04q")
	if got, want := state.lines[0], "é?e\u0301"; got != want {
		t.Fatalf("terminal text=%q, want %q", got, want)
	}
}

func TestShortcutsRespectModalScopeAndModifierIdentity(t *testing.T) {
	state := newTestEditor(t)
	state.active = 1
	state.startEditing()
	before := state.input.Value()
	for _, key := range []tea.KeyPressMsg{
		{Code: 'x', Mod: tea.ModCtrl, Text: "x"},
		{Code: 'd', Mod: tea.ModSuper, Text: "d"},
		{Code: 'm', Mod: tea.ModCtrl | tea.ModAlt, Text: "m"},
		{Code: tea.KeySpace, Mod: tea.ModCtrl, Text: " "},
	} {
		state.Update(key)
	}
	if state.input.Value() != before || !state.numberChosen || state.muted || !state.editing {
		t.Fatalf("unbound modified keys changed editor: input=%q, muted=%t, editing=%t", state.input.Value(), state.muted, state.editing)
	}
	state.openPresetSave()
	clauses := len(state.lines)
	state.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt, Text: "d"})
	state.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModCtrl, Text: "m"})
	if len(state.lines) != clauses || state.muted || state.presetMode != presetSaving || state.presetInput.Value() != "" {
		t.Fatal("editor shortcut leaked into the preset dialog")
	}
	state.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "c"})
	if state.presetMode != presetClosed {
		t.Fatal("Ctrl+C with associated text did not cancel the modal")
	}
}

func TestRepeatStillNavigatesAndNudges(t *testing.T) {
	state := newTestEditor(t)
	state.Update(tea.KeyPressMsg{Code: tea.KeyDown, IsRepeat: true})
	if state.active != 1 {
		t.Fatal("repeated navigation did not move")
	}
	state.startEditing()
	state.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt, IsRepeat: true})
	if !strings.Contains(state.lines[1], "index=4.04") {
		t.Fatalf("repeated nudge: %q", state.lines[1])
	}
}

func TestLogicalShortcutWinsOverAlternateLayoutCode(t *testing.T) {
	state := newTestEditor(t)
	initial := len(state.lines)
	state.Update(tea.KeyPressMsg{Code: 'd', BaseCode: 'g', Mod: tea.ModCtrl | tea.ModShift, Text: "D"})
	if len(state.lines) != initial-1 || state.presetMode != presetClosed {
		t.Fatal("alternate layout code stole the logical delete shortcut")
	}
	state.Update(tea.KeyPressMsg{Code: 'д', BaseCode: 'm', Mod: tea.ModCtrl})
	if !state.muted {
		t.Fatal("alternate layout code did not resolve an otherwise unbound shortcut")
	}
}

func TestRepeatedPresetConfirmationDoesNotOverwrite(t *testing.T) {
	state := newTestEditor(t)
	state.config.PresetDir = t.TempDir()
	if _, _, err := savePreset(state.config.PresetDir, "Bass", "stash old.source"); err != nil {
		t.Fatal(err)
	}
	state.openPresetSave()
	state.presetInput.SetValue("Bass")
	state.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	state.Update(tea.KeyPressMsg{Code: tea.KeyEnter, IsRepeat: true})
	if state.presetMode != presetSaving || !state.presetOverwrite {
		t.Fatal("held Enter accepted the overwrite confirmation")
	}
	items, err := listPresets(state.config.PresetDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("presets=%v, error=%v", items, err)
	}
	lines, err := loadPreset(items[0])
	if err != nil || Command(lines) != "stash old.source" {
		t.Fatalf("repeat overwrote preset: lines=%v, error=%v", lines, err)
	}
}

func TestShortcutHintsTrackActualDisambiguation(t *testing.T) {
	state := newTestEditor(t)
	for _, test := range []struct {
		flags    int
		enhanced bool
	}{
		{1, true}, {7, true}, {8, true}, {2, false}, {4, false}, {16, false}, {0, false},
	} {
		state.Update(tea.KeyboardEnhancementsMsg{Flags: test.flags})
		if state.enhancedKeys != test.enhanced {
			t.Fatalf("flags=%d: enhanced=%t", test.flags, state.enhancedKeys)
		}
		want := "alt+m"
		if test.enhanced {
			want = "ctrl+m"
		}
		if state.shortcutHint(muteAction) != want || !strings.Contains(state.helpView(80), want) {
			t.Fatalf("flags=%d: mute hint=%q, want %q", test.flags, state.shortcutHint(muteAction), want)
		}
	}
}

func TestShortcutScopesHaveNoConflictingBindings(t *testing.T) {
	for _, scope := range []keyScope{navigationKeys, editingKeys, saveKeys, loadKeys} {
		seen := make(map[keystroke]keyAction)
		for _, binding := range shortcuts {
			if binding.scope&scope == 0 {
				continue
			}
			for _, stroke := range binding.keys {
				if previous, exists := seen[stroke]; exists {
					t.Fatalf("scope=%d, key=%v: actions %d and %d conflict", scope, stroke, previous, binding.action)
				}
				seen[stroke] = binding.action
			}
		}
	}
}
