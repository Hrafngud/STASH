package source

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Entry combines stable source metadata with current backend availability.
// An unavailable entry remains discoverable and cannot construct a collector.
type Entry struct {
	Info              Info
	Available         bool
	UnavailableReason string
	factory           Factory
}

// UnavailableError reports that a known source cannot be collected on the
// current system.
type UnavailableError struct {
	Name   string
	Reason string
}

func (err *UnavailableError) Error() string {
	if err.Reason == "" {
		return fmt.Sprintf("source %s unavailable on this system", err.Name)
	}
	return fmt.Sprintf("source %s unavailable on this system: %s", err.Name, err.Reason)
}

// Registry owns source discovery, availability, metadata, and collector
// construction.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]Entry)}
}

// RegisterAvailable adds a collectable source definition.
func (registry *Registry) RegisterAvailable(info Info, factory Factory) error {
	if factory == nil {
		return fmt.Errorf("register source %q: collector factory is nil", info.Name)
	}
	return registry.register(Entry{Info: cloneInfo(info), Available: true, factory: factory})
}

// RegisterUnavailable adds a known but currently unavailable source.
func (registry *Registry) RegisterUnavailable(info Info, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("register source %q: unavailable reason is empty", info.Name)
	}
	return registry.register(Entry{
		Info:              cloneInfo(info),
		UnavailableReason: reason,
	})
}

func (registry *Registry) register(entry Entry) error {
	if registry == nil {
		return fmt.Errorf("register source %q: registry is nil", entry.Info.Name)
	}
	if err := validateInfo(entry.Info); err != nil {
		return fmt.Errorf("register source %q: %w", entry.Info.Name, err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.entries[entry.Info.Name]; exists {
		return fmt.Errorf("register source %q: name is already registered", entry.Info.Name)
	}
	registry.entries[entry.Info.Name] = entry
	return nil
}

// Lookup returns a copy of a registered entry.
func (registry *Registry) Lookup(name string) (Entry, bool) {
	if registry == nil {
		return Entry{}, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	entry, ok := registry.entries[name]
	if !ok {
		return Entry{}, false
	}
	entry.Info = cloneInfo(entry.Info)
	return entry, true
}

// List returns entries in stable bytewise source-name order.
func (registry *Registry) List() []Entry {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	entries := make([]Entry, 0, len(registry.entries))
	for _, entry := range registry.entries {
		entry.Info = cloneInfo(entry.Info)
		entries = append(entries, entry)
	}
	registry.mu.RUnlock()
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Info.Name < entries[right].Info.Name
	})
	return entries
}

// NewCollector constructs an independent collector for a known, available
// source.
func (registry *Registry) NewCollector(ctx context.Context, name string) (Collector, error) {
	entry, ok := registry.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown source: %s", name)
	}
	if !entry.Available {
		return nil, &UnavailableError{Name: name, Reason: entry.UnavailableReason}
	}
	collector, err := entry.factory(ctx)
	if err != nil {
		return nil, fmt.Errorf("open source %s: %w", name, err)
	}
	if collector == nil {
		return nil, fmt.Errorf("open source %s: collector factory returned nil", name)
	}
	return collector, nil
}

func validateInfo(info Info) error {
	if info.Name == "" {
		return fmt.Errorf("name is empty")
	}
	if strings.TrimSpace(info.Name) != info.Name || strings.ContainsAny(info.Name, " \t\r\n") {
		return fmt.Errorf("name %q contains whitespace", info.Name)
	}
	switch info.Kind {
	case KindScalar, KindVector:
	default:
		return fmt.Errorf("invalid kind %q", info.Kind)
	}
	if info.Unit == "" {
		return fmt.Errorf("unit is empty")
	}
	if (info.NaturalMin == nil) != (info.NaturalMax == nil) {
		return fmt.Errorf("natural range requires both minimum and maximum")
	}
	if info.NaturalMin != nil {
		minimum, maximum := *info.NaturalMin, *info.NaturalMax
		if math.IsNaN(minimum) || math.IsInf(minimum, 0) || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
			return fmt.Errorf("natural range must be finite")
		}
		if minimum >= maximum {
			return fmt.Errorf("natural range minimum must be less than maximum")
		}
	}
	return nil
}

func cloneInfo(info Info) Info {
	cloned := info
	if info.NaturalMin != nil {
		minimum := *info.NaturalMin
		cloned.NaturalMin = &minimum
	}
	if info.NaturalMax != nil {
		maximum := *info.NaturalMax
		cloned.NaturalMax = &maximum
	}
	return cloned
}
