package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/zalmo/stash/internal/cli"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/source"
)

// Suggestion is a semantic completion and the contextual help shown with it.
// Value replaces the complete active clause, avoiding ambiguous token edits.
type Suggestion struct {
	Label string
	Value string
	Help  string
}

var optionHelp = []Suggestion{
	{"-s", "-s ", "add a synth node"},
	{"-m", "-m ", "route telemetry, rhythm, or syn.ID.out to a parameter"},
	{"--range", "--range ", "override a telemetry/rhythm control's input range"},
	{"-w", "-w ", "select an oscillator waveform"},
	{"-v", "-v " + numberText(cli.DefaultGain), "set output or synth-master gain"},
	{"-t", "-t ", "define a telemetry trigger"},
	{"-n", "-n ", "define notes, a scale, or a mode"},
	{"-r", "-r ", "define a rhythm"},
	{"-b", "-b ", "set tempo in BPM"},
	{"-d", "-d " + cli.DefaultGateDuration.String(), "set event gate duration"},
	{"-a", "-a " + defaultADSRValue(), "set attack,decay,sustain,release"},
	{"--swing", "--swing " + numberText(primitive.DefaultSwing), "set swing percentage (50..75)"},
	{"-f", "-f ", "append a filter"},
	{"-x", "-x ", "append an effect"},
}

func defaultADSRValue() string {
	value := cli.DefaultADSR
	return strings.Join([]string{value.Attack.String(), value.Decay.String(), numberText(value.Sustain), value.Release.String()}, ",")
}

// Complete resolves suggestions from the clause context and declarations in
// the entire partially valid document.
func Complete(registry *source.Registry, lines []string, active int) []Suggestion {
	if active < 0 || active >= len(lines) {
		return nil
	}
	line := strings.TrimLeft(lines[active], " \t")
	if line == "" {
		if active == 0 || !hasSourceClause(lines) {
			return sourceSuggestions(registry, "", "")
		}
		return optionSuggestions(lines)
	}
	if !strings.HasPrefix(line, "-") {
		return sourceSuggestions(registry, line, "")
	}
	option, value, hasValue := strings.Cut(line, " ")
	if !hasValue {
		return filterSuggestions(optionSuggestions(lines), option)
	}
	switch option {
	case "-s":
		return synthSuggestions(value)
	case "-m":
		return modulationSuggestions(registry, lines, value)
	case "--range":
		if before, _, found := strings.Cut(value, "="); !found {
			return sourceSuggestions(registry, before, "--range ", "=")
		}
	case "-w":
		return literalSuggestions("-w ", value, []string{"sine", "square", "saw", "tri", "noise"}, "oscillator waveform")
	case "-t":
		return literalSuggestions("-t ", value, []string{"above:", "below:", "rise:", "fall:"}, "trigger threshold")
	case "-n":
		return literalSuggestions("-n ", value, []string{"C4", "scale:C4:major:8", "mode:E3:phrygian:12"}, "note material")
	case "-r":
		return literalSuggestions("-r ", value, []string{"rhythm:120:1/8:x-x-x-x-", "rhythm:1/16:x---x---x-x-x---"}, "rhythm primitive")
	case "-f":
		return effectSuggestions("-f ", value, "filter")
	case "-x":
		return effectSuggestions("-x ", value, "effect")
	}
	return nil
}

func optionSuggestions(lines []string) []Suggestion {
	result := append([]Suggestion(nil), optionHelp...)
	if !hasOption(lines, "-s") {
		return result
	}
	for index := range result {
		if result[index].Label == "-v" {
			result[index].Value = "-v 1"
			break
		}
	}
	return result
}

func hasSourceClause(lines []string) bool {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "-") {
			return true
		}
	}
	return false
}

func sourceSuggestions(registry *source.Registry, prefix, clausePrefix string, suffix ...string) []Suggestion {
	if registry == nil {
		return nil
	}
	appendix := ""
	if len(suffix) > 0 {
		appendix = suffix[0]
	}
	var result []Suggestion
	for _, entry := range registry.List() {
		if !strings.HasPrefix(entry.Info.Name, prefix) {
			continue
		}
		rangeText := "natural range: unspecified"
		if entry.Info.NaturalMin != nil {
			rangeText = fmt.Sprintf("natural range: %g..%g", *entry.Info.NaturalMin, *entry.Info.NaturalMax)
		}
		availability := "available"
		if !entry.Available {
			availability = "unavailable: " + entry.UnavailableReason
		}
		result = append(result, Suggestion{
			Label: entry.Info.Name, Value: clausePrefix + entry.Info.Name + appendix,
			Help: fmt.Sprintf("telemetry control · %s %s\n%s\n%s", entry.Info.Kind, entry.Info.Unit, rangeText, availability),
		})
	}
	return result
}

func synthSuggestions(value string) []Suggestion {
	head, tail, hasComma := strings.Cut(value, ",")
	if !hasComma {
		typeToken, id, hasID := strings.Cut(head, ":")
		if !hasID {
			var result []Suggestion
			for _, kind := range sound.SynthTypes() {
				if !strings.HasPrefix(string(kind), typeToken) {
					continue
				}
				spec, _ := sound.LookupSynthSpec(kind)
				result = append(result, Suggestion{Label: string(kind), Value: "-s " + string(kind) + ":", Help: spec.Description + "\nchoose an instance id"})
			}
			return result
		}
		if _, ok := sound.LookupSynthSpec(sound.SynthType(typeToken)); !ok {
			return nil
		}
		ids := []string{"bass", "lead", "voice", "mod", typeToken + "1"}
		return literalSuggestions("-s "+typeToken+":", id, ids, "synth instance id; comma continues to parameters")
	}
	typeToken, _, _ := strings.Cut(head, ":")
	spec, ok := sound.LookupSynthSpec(sound.SynthType(typeToken))
	if !ok {
		return nil
	}
	segments := strings.Split(tail, ",")
	current := segments[len(segments)-1]
	base := "-s " + head + ","
	if len(segments) > 1 {
		base += strings.Join(segments[:len(segments)-1], ",") + ","
	}
	name, entered, hasEquals := strings.Cut(current, "=")
	if hasEquals {
		if config, exists := spec.Config[name]; exists {
			values := configValues(name)
			if len(values) > 0 {
				return literalSuggestions(base+name+"=", entered, values, config.Description)
			}
		}
		return nil
	}
	var result []Suggestion
	for _, parameterName := range sound.SortedParameterNames(spec) {
		if strings.HasPrefix(parameterName, name) {
			parameter := spec.Parameters[parameterName]
			result = append(result, Suggestion{Label: parameterName + "=", Value: base + parameterName + "=" + parameterValueText(parameter.Default, parameter.Unit), Help: parameterHelp(parameter.Description, parameter.Unit, parameter.Minimum, parameter.Maximum, parameter.Default, parameter.AudioRate)})
		}
	}
	configNames := make([]string, 0, len(spec.Config))
	for configName := range spec.Config {
		configNames = append(configNames, configName)
	}
	sort.Strings(configNames)
	for _, configName := range configNames {
		if strings.HasPrefix(configName, name) {
			config := spec.Config[configName]
			value := base + configName + "="
			if _, err := strconv.ParseFloat(config.Default, 64); err == nil {
				value += config.Default
			}
			result = append(result, Suggestion{Label: configName + "=", Value: value, Help: config.Description + "\ngraph-time setting"})
		}
	}
	return result
}

func configValues(name string) []string {
	switch name {
	case "wave", "modwave":
		return []string{"sine", "square", "saw", "tri", "noise"}
	case "filter":
		return []string{"lp", "hp"}
	case "model":
		return []string{"metal", "wood", "glass", "bell", "plate"}
	case "table":
		return []string{"metal", "digital", "smooth"}
	}
	return nil
}

func parameterHelp(description, unit string, minimum, maximum *float64, defaultValue float64, audioRate bool) string {
	minimumText, maximumText := "unbounded", "unbounded"
	if minimum != nil {
		minimumText = strconv.FormatFloat(*minimum, 'g', -1, 64)
	}
	if maximum != nil {
		maximumText = strconv.FormatFloat(*maximum, 'g', -1, 64)
	}
	return fmt.Sprintf("%s\nrange: %s..%s %s\ndefault: %g\naudio-rate: %t", description, minimumText, maximumText, unit, defaultValue, audioRate)
}

func effectSuggestions(clausePrefix, value, targetKind string) []Suggestion {
	name, arguments, hasColon := strings.Cut(value, ":")
	var specs []sound.EffectSpec
	for _, spec := range sound.EffectSpecs() {
		if spec.Target == targetKind || targetKind == "effect" && spec.Target != "filter" {
			specCopy := spec
			specs = append(specs, specCopy)
		}
	}
	if !hasColon {
		var result []Suggestion
		for _, spec := range specs {
			if strings.HasPrefix(spec.Name, name) {
				result = append(result, Suggestion{Label: spec.Name, Value: effectDefaultValue(clausePrefix, spec), Help: effectSpecHelp(spec)})
			}
		}
		return result
	}
	var spec sound.EffectSpec
	found := false
	for _, candidate := range specs {
		if candidate.Name == name {
			spec, found = candidate, true
			break
		}
	}
	if !found {
		return nil
	}
	segments := strings.Split(arguments, ",")
	current := segments[len(segments)-1]
	if strings.Contains(current, "=") {
		return nil
	}
	base := clausePrefix + name + ":"
	if len(segments) > 1 {
		base += strings.Join(segments[:len(segments)-1], ",") + ","
	}
	var result []Suggestion
	for _, parameter := range spec.Parameters {
		if strings.HasPrefix(parameter.Name, current) {
			result = append(result, Suggestion{Label: parameter.Name + "=", Value: base + parameter.Name + "=" + parameterValueText(parameter.Default, parameter.Unit), Help: effectParameterHelp(spec, parameter)})
		}
	}
	return result
}

func effectDefaultValue(clausePrefix string, spec sound.EffectSpec) string {
	if spec.Kind == sound.EffectConvolution {
		return clausePrefix + spec.Name + ":"
	}
	arguments := make([]string, len(spec.Parameters))
	for index, parameter := range spec.Parameters {
		arguments[index] = parameter.Name + "=" + parameterValueText(parameter.Default, parameter.Unit)
	}
	return clausePrefix + spec.Name + ":" + strings.Join(arguments, ",")
}

func numberText(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func parameterValueText(value float64, unit string) string {
	text := numberText(value)
	if unit == "s" {
		text += "s"
	}
	return text
}

func effectSpecHelp(spec sound.EffectSpec) string {
	names := make([]string, len(spec.Parameters))
	for index, parameter := range spec.Parameters {
		names[index] = parameter.Name
	}
	return "parameters: " + strings.Join(names, ", ")
}

func effectParameterHelp(spec sound.EffectSpec, parameter sound.EffectParameter) string {
	return parameterHelp(spec.Name+" "+parameter.Name, parameter.Unit, parameter.Minimum, parameter.Maximum, parameter.Default, false)
}

func modulationSuggestions(registry *source.Registry, lines []string, value string) []Suggestion {
	left, _, hasMap := strings.Cut(value, "=")
	if hasMap {
		return nil
	}
	control, targetPrefix, hasControl := strings.Cut(left, ":")
	if !hasControl {
		var result []Suggestion
		for _, item := range controlSuggestions(registry, lines, control) {
			item.Value = "-m " + item.Value + ":"
			result = append(result, item)
		}
		for _, item := range targetSuggestions(lines, control) {
			item.Value = "-m " + item.Value + "="
			result = append(result, item)
		}
		return result
	}
	result := targetSuggestions(lines, targetPrefix)
	for index := range result {
		result[index].Value = "-m " + control + ":" + result[index].Value + "="
	}
	return result
}

func controlSuggestions(registry *source.Registry, lines []string, prefix string) []Suggestion {
	result := sourceSuggestions(registry, prefix, "")
	for _, control := range []string{"rhythm.gate", "rhythm.hit", "rhythm.step", "rhythm.velocity", "rhythm.phase"} {
		if strings.HasPrefix(control, prefix) && hasOption(lines, "-r") {
			result = append(result, Suggestion{Label: control, Value: control, Help: "rhythm control"})
		}
	}
	for _, synth := range partialSynths(lines) {
		name := "syn." + synth.ID + ".out"
		if strings.HasPrefix(name, prefix) {
			result = append(result, Suggestion{Label: name, Value: name, Help: "audio-rate synth signal; unlike telemetry, only audio-rate targets accept it"})
		}
	}
	return result
}

func targetSuggestions(lines []string, prefix string) []Suggestion {
	var result []Suggestion
	seen := map[string]bool{}
	add := func(name, help string) {
		if !seen[name] && strings.HasPrefix(name, prefix) {
			seen[name] = true
			result = append(result, Suggestion{Label: name, Value: name, Help: help})
		}
	}
	synths := partialSynths(lines)
	if len(synths) == 0 {
		for _, name := range []string{"freq", "gain", "pan", "gate"} {
			add(name, "legacy voice parameter")
		}
	}
	for _, synth := range synths {
		spec := synth.Spec()
		for _, name := range sound.SortedSynthParameterNames(synth) {
			parameter, known := spec.Parameters[name]
			help := "synth parameter"
			if known {
				help = parameterHelp(parameter.Description, parameter.Unit, parameter.Minimum, parameter.Maximum, parameter.Default, parameter.AudioRate)
			}
			add("syn."+synth.ID+"."+name, help)
			add("syn."+synth.ID+"."+name+".mod", help+"\nadditive inlet")
		}
	}
	for _, effect := range partialEffects(lines) {
		spec, ok := sound.LookupEffectSpec(effect.Kind)
		if !ok {
			continue
		}
		for _, parameter := range spec.Parameters {
			add(spec.Target+"."+parameter.Name, effectParameterHelp(spec, parameter))
		}
	}
	return result
}

func partialSynths(lines []string) []sound.Synth {
	var synths []sound.Synth
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "-s ") {
			continue
		}
		synth, err := sound.ParseSynth(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-s ")))
		if err == nil {
			synths = append(synths, synth)
		}
	}
	if sound.AssignSynthIDs(synths) != nil {
		return nil
	}
	return synths
}

func partialEffects(lines []string) []sound.Effect {
	var effects []sound.Effect
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		var effect sound.Effect
		var err error
		switch {
		case strings.HasPrefix(line, "-f "):
			effect, err = sound.ParseFilter(strings.TrimSpace(strings.TrimPrefix(line, "-f ")))
		case strings.HasPrefix(line, "-x "):
			effect, err = sound.ParseEffect(strings.TrimSpace(strings.TrimPrefix(line, "-x ")))
		default:
			continue
		}
		if err == nil {
			effects = append(effects, effect)
		}
	}
	return effects
}

func hasOption(lines []string, option string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), option+" ") {
			return true
		}
	}
	return false
}

func literalSuggestions(base, prefix string, values []string, help string) []Suggestion {
	var result []Suggestion
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			result = append(result, Suggestion{Label: value, Value: base + value, Help: help})
		}
	}
	return result
}

func filterSuggestions(input []Suggestion, prefix string) []Suggestion {
	var result []Suggestion
	for _, item := range input {
		if strings.HasPrefix(item.Label, prefix) {
			result = append(result, item)
		}
	}
	return result
}
