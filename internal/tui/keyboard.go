package tui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

type keyScope uint8

const (
	navigationKeys keyScope = 1 << iota
	editingKeys
	saveKeys
	loadKeys
	editorKeys = navigationKeys | editingKeys
	presetKeys = saveKeys | loadKeys
)

type keyAction uint8

const (
	noAction keyAction = iota
	quitAction
	muteAction
	oscillatorAction
	saveAction
	loadAction
	deleteClauseAction
	insertBelowAction
	insertAboveAction
	editAction
	doneAction
	previousAction
	nextAction
	moveUpAction
	moveDownAction
	acceptAction
	cancelAction
	nudgeUpAction
	nudgeDownAction
	numberLeftAction
	numberRightAction
	cursorLeftAction
	cursorRightAction
	cursorMoveAction
	deleteNumberAction
	pasteAction
)

type keystroke struct {
	code rune
	mod  tea.KeyMod
}

type shortcut struct {
	scope  keyScope
	action keyAction
	keys   []keystroke
}

// Application shortcuts are resolved before textinput bindings. Modal scopes
// replace editor scopes, so an editor action cannot leak into a preset dialog.
// Key codes and modifiers, rather than associated text, identify shortcuts.
var shortcuts = []shortcut{
	{editorKeys, quitAction, []keystroke{{'c', tea.ModCtrl}}},
	{navigationKeys, quitAction, []keystroke{{'q', 0}, {tea.KeyEsc, 0}}},
	{editorKeys, muteAction, []keystroke{{'m', tea.ModAlt}, {'m', tea.ModCtrl}}},
	{editorKeys, oscillatorAction, []keystroke{{'o', tea.ModCtrl}}},
	{editorKeys, saveAction, []keystroke{{'g', tea.ModCtrl}}},
	{editorKeys, loadAction, []keystroke{{'g', tea.ModAlt}, {'g', tea.ModCtrl | tea.ModShift}}},
	{editorKeys, deleteClauseAction, []keystroke{{'d', tea.ModAlt}, {'d', tea.ModCtrl | tea.ModShift}}},
	{editorKeys, insertBelowAction, []keystroke{{'n', tea.ModCtrl}}},
	{navigationKeys, insertBelowAction, []keystroke{{'a', 0}, {'o', 0}}},
	{editorKeys, insertAboveAction, []keystroke{{'p', tea.ModCtrl}}},
	{navigationKeys, editAction, []keystroke{{tea.KeyEnter, 0}, {'e', 0}}},
	{editingKeys, doneAction, []keystroke{{'d', tea.ModCtrl}}},
	{navigationKeys | loadKeys, previousAction, []keystroke{{tea.KeyUp, 0}, {'k', 0}}},
	{editingKeys | loadKeys, previousAction, []keystroke{{tea.KeyTab, tea.ModShift}}},
	{editingKeys, previousAction, []keystroke{{tea.KeyUp, 0}}},
	{navigationKeys | loadKeys, nextAction, []keystroke{{tea.KeyDown, 0}, {'j', 0}}},
	{editingKeys | loadKeys, nextAction, []keystroke{{tea.KeyTab, 0}}},
	{editingKeys, nextAction, []keystroke{{tea.KeyDown, 0}}},
	{navigationKeys, moveUpAction, []keystroke{{tea.KeyUp, tea.ModCtrl}}},
	{navigationKeys, moveDownAction, []keystroke{{tea.KeyDown, tea.ModCtrl}}},
	{editingKeys | presetKeys, acceptAction, []keystroke{{tea.KeyEnter, 0}}},
	{presetKeys, cancelAction, []keystroke{{tea.KeyEsc, 0}, {'c', tea.ModCtrl}}},
	{editingKeys, nudgeUpAction, []keystroke{{tea.KeyUp, tea.ModAlt}}},
	{editingKeys, nudgeDownAction, []keystroke{{tea.KeyDown, tea.ModAlt}}},
	{editingKeys, numberLeftAction, []keystroke{{tea.KeyLeft, tea.ModAlt}}},
	{editingKeys, numberRightAction, []keystroke{{tea.KeyRight, tea.ModAlt}}},
	{editingKeys, cursorLeftAction, []keystroke{{tea.KeyLeft, 0}}},
	{editingKeys, cursorRightAction, []keystroke{{tea.KeyRight, 0}}},
	{editingKeys, cursorMoveAction, []keystroke{
		{tea.KeyHome, 0}, {tea.KeyEnd, 0}, {'a', tea.ModCtrl}, {'e', tea.ModCtrl},
		{'b', tea.ModCtrl}, {'f', tea.ModCtrl}, {tea.KeyLeft, tea.ModCtrl}, {tea.KeyRight, tea.ModCtrl},
	}},
	{editingKeys, deleteNumberAction, []keystroke{{tea.KeyBackspace, 0}, {tea.KeyDelete, 0}}},
	{editingKeys, pasteAction, []keystroke{{'v', tea.ModCtrl}}},
}

const commandModifiers = tea.ModCtrl | tea.ModAlt | tea.ModMeta | tea.ModHyper | tea.ModSuper

func normalizeKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	// The input decoder used by Bubble Tea v2.0.9 maps Kitty's key code 0
	// (text-only/IME input) to the legacy Ctrl+Space alias. Associated text
	// other than a space identifies that text event; retain it as input.
	if key.Code == tea.KeySpace && key.Mod == tea.ModCtrl && key.Text != "" && key.Text != " " {
		key.Code, key.Mod = 0, 0
		return key
	}
	if key.Mod&commandModifiers != 0 {
		// String() prefers Text, including for modified keys. Bubbles also
		// uses String() for its bindings, so command text must not reach it.
		key.Text = ""
		key.Code = unicode.ToLower(key.Code)
		key.BaseCode = unicode.ToLower(key.BaseCode)
		return key
	}
	if key.Text == "" && unicode.IsPrint(key.Code) {
		code := key.Code
		if key.Mod.Contains(tea.ModShift) || key.Mod.Contains(tea.ModCapsLock) {
			if key.ShiftedCode != 0 {
				code = key.ShiftedCode
			} else {
				code = unicode.ToUpper(code)
			}
		}
		key.Text = string(code)
	}
	return key
}

func resolveShortcut(key tea.KeyPressMsg, scope keyScope) keyAction {
	mod := key.Mod & (commandModifiers | tea.ModShift)
	action := matchShortcut(key, key.Code, mod, scope)
	if action == noAction && mod != 0 && key.BaseCode != 0 {
		// The logical key takes precedence. A physical-layout fallback must
		// not steal a different binding that already matched the logical key.
		action = matchShortcut(key, key.BaseCode, mod, scope)
	}
	return action
}

func matchShortcut(key tea.KeyPressMsg, code rune, mod tea.KeyMod, scope keyScope) keyAction {
	for _, binding := range shortcuts {
		if binding.scope&scope == 0 {
			continue
		}
		for _, stroke := range binding.keys {
			if mod != stroke.mod {
				continue
			}
			if mod == 0 && unicode.IsPrint(stroke.code) && key.Text != "" {
				if key.Text == string(stroke.code) {
					return binding.action
				}
				continue
			}
			if code == stroke.code {
				return binding.action
			}
		}
	}
	return noAction
}

// Toggles, structural edits, and dialog confirmations fire once per press.
// Navigation and value adjustments can repeat while the key is held down.
func (action keyAction) repeats() bool {
	switch action {
	case previousAction, nextAction, moveUpAction, moveDownAction,
		nudgeUpAction, nudgeDownAction, numberLeftAction, numberRightAction,
		cursorLeftAction, cursorRightAction, cursorMoveAction, deleteNumberAction:
		return true
	}
	return false
}

func (state *editor) keyScope() keyScope {
	switch state.presetMode {
	case presetSaving:
		return saveKeys
	case presetLoading:
		return loadKeys
	}
	if state.editing {
		return editingKeys
	}
	return navigationKeys
}

func (state *editor) shortcutHint(action keyAction) string {
	for _, binding := range shortcuts {
		if binding.action != action || binding.scope&state.keyScope() == 0 {
			continue
		}
		stroke := binding.keys[0]
		if state.enhancedKeys && len(binding.keys) > 1 && stroke.mod == tea.ModAlt {
			stroke = binding.keys[1]
		}
		return (tea.KeyPressMsg{Code: stroke.code, Mod: stroke.mod}).Keystroke()
	}
	return ""
}
