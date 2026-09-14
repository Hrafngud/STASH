package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const presetExtension = ".stash"

type preset struct {
	name string
	path string
}

func defaultPresetDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate preset directory: %w", err)
	}
	return filepath.Join(configDir, "stash", "presets"), nil
}

func presetName(input string) (string, error) {
	name := strings.TrimSpace(input)
	name = strings.TrimSpace(strings.TrimSuffix(name, presetExtension))
	if name == "" {
		return "", fmt.Errorf("preset name is required")
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("preset name cannot begin with a dot")
	}
	if utf8.RuneCountInString(name) > 64 {
		return "", fmt.Errorf("preset name must be 64 characters or fewer")
	}
	for _, character := range name {
		if character == '/' || character == '\\' || unicode.IsControl(character) {
			return "", fmt.Errorf("preset name cannot contain slashes or control characters")
		}
	}
	return name, nil
}

func savePreset(directory, input, command string) (name string, replaced bool, err error) {
	name, err = presetName(input)
	if err != nil {
		return "", false, err
	}
	if directory == "" {
		return "", false, fmt.Errorf("preset directory is not configured")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", false, fmt.Errorf("create preset directory: %w", err)
	}

	path := filepath.Join(directory, name+presetExtension)
	if _, statErr := os.Stat(path); statErr == nil {
		replaced = true
	} else if !os.IsNotExist(statErr) {
		return "", false, fmt.Errorf("inspect preset: %w", statErr)
	}

	temporary, err := os.CreateTemp(directory, ".preset-*.tmp")
	if err != nil {
		return "", false, fmt.Errorf("create temporary preset: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", false, fmt.Errorf("protect preset: %w", err)
	}
	if _, err := temporary.WriteString(strings.TrimSpace(command) + "\n"); err != nil {
		_ = temporary.Close()
		return "", false, fmt.Errorf("write preset: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", false, fmt.Errorf("close preset: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", false, fmt.Errorf("save preset: %w", err)
	}
	return name, replaced, nil
}

func listPresets(directory string) ([]preset, error) {
	if directory == "" {
		return nil, fmt.Errorf("preset directory is not configured")
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list presets: %w", err)
	}
	presets := make([]preset, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), presetExtension) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		presets = append(presets, preset{name: name, path: filepath.Join(directory, entry.Name())})
	}
	sort.Slice(presets, func(left, right int) bool {
		return strings.ToLower(presets[left].name) < strings.ToLower(presets[right].name)
	})
	return presets, nil
}

func loadPreset(item preset) ([]string, error) {
	contents, err := os.ReadFile(item.path)
	if err != nil {
		return nil, fmt.Errorf("read preset %q: %w", item.name, err)
	}
	lines, err := pastedCommandLines(string(contents))
	if err != nil {
		return nil, fmt.Errorf("read preset %q: %w", item.name, err)
	}
	return lines, nil
}
