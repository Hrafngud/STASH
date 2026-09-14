package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPresetStoreRoundTripAndSort(t *testing.T) {
	directory := t.TempDir()
	for _, item := range []struct {
		name    string
		command string
	}{
		{name: "zebra", command: "stash cpu.usage -w saw"},
		{name: "Ambient Pad.stash", command: "stash cpu.usage -w sine"},
	} {
		if _, _, err := savePreset(directory, item.name, item.command); err != nil {
			t.Fatal(err)
		}
	}
	presets, err := listPresets(directory)
	if err != nil {
		t.Fatal(err)
	}
	gotNames := []string{presets[0].name, presets[1].name}
	if want := []string{"Ambient Pad", "zebra"}; !reflect.DeepEqual(gotNames, want) {
		t.Fatalf("preset names = %v, want %v", gotNames, want)
	}
	lines, err := loadPreset(presets[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cpu.usage", "-w sine"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("loaded lines = %v, want %v", lines, want)
	}
	info, err := os.Stat(filepath.Join(directory, "Ambient Pad.stash"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preset permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestPresetNameRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"", ".hidden", "../outside", `folder\preset`} {
		if _, err := presetName(name); err == nil {
			t.Errorf("presetName(%q) unexpectedly succeeded", name)
		}
	}
}
