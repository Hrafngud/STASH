package cli

import (
	"fmt"
	"strings"

	"github.com/zalmo/stash/internal/control"
	"github.com/zalmo/stash/internal/primitive"
	"github.com/zalmo/stash/internal/sound"
	"github.com/zalmo/stash/internal/unit"
)

var sourceOptions = map[string]struct{}{
	"-w": {}, "-m": {}, "--range": {}, "-v": {},
	"-t": {}, "-n": {}, "-r": {}, "-b": {}, "-d": {},
	"-a": {}, "--swing": {}, "-f": {}, "-x": {}, "-o": {},
}

// Parse parses one complete argv (without argv[0]) according to the public
// command forms. Discovery commands are exclusive and source options must
// follow the positional source.
func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("missing command: expected SOURCE, -l, -i, or -p")
	}

	switch args[0] {
	case "-l":
		if len(args) > 2 {
			return Command{}, fmt.Errorf("option -l accepts at most one PREFIX")
		}
		command := Command{Kind: CommandList}
		if len(args) == 2 {
			if args[1] == "" {
				return Command{}, fmt.Errorf("option -l PREFIX must not be empty")
			}
			if _, isOption := sourceOptions[args[1]]; isOption || args[1] == "-i" || args[1] == "-p" {
				return Command{}, fmt.Errorf("option -l cannot be combined with option %s", args[1])
			}
			if strings.HasPrefix(args[1], "-") && args[1] != "-" {
				return Command{}, fmt.Errorf("unknown option %q", args[1])
			}
			command.ListPrefix = args[1]
		}
		return command, nil
	case "-i", "-p":
		if len(args) != 2 || args[1] == "" {
			return Command{}, fmt.Errorf("option %s requires exactly one argument", args[0])
		}
		if args[0] == "-i" {
			return Command{Kind: CommandInspect, InspectSource: args[1]}, nil
		}
		return Command{Kind: CommandPrimitive, Primitive: args[1]}, nil
	}

	if strings.HasPrefix(args[0], "-") && args[0] != "-" {
		return Command{}, fmt.Errorf("unknown option %q: expected SOURCE before options", args[0])
	}
	if args[0] == "" {
		return Command{}, fmt.Errorf("source must not be empty")
	}

	command := Command{Kind: CommandSource, Source: args[0]}
	seen := make(map[string]bool)
	for index := 1; index < len(args); index++ {
		option := args[index]
		if option == "-l" || option == "-i" || option == "-p" {
			return Command{}, fmt.Errorf("discovery option %s cannot be combined with source %s", option, command.Source)
		}
		if _, ok := sourceOptions[option]; !ok {
			if strings.HasPrefix(option, "-") {
				return Command{}, fmt.Errorf("unknown option %q", option)
			}
			return Command{}, fmt.Errorf("unexpected positional argument %q", option)
		}

		value, err := optionValue(args, &index, option)
		if err != nil {
			return Command{}, err
		}
		if option != "-m" && option != "-f" && option != "-x" {
			if seen[option] {
				return Command{}, fmt.Errorf("duplicate option %s", option)
			}
			seen[option] = true
		}

		switch option {
		case "-w":
			waveform, err := parseWaveform(value)
			if err != nil {
				return Command{}, err
			}
			command.Waveform = &waveform
		case "-m":
			modulation, err := parseModulation(value)
			if err != nil {
				return Command{}, err
			}
			command.Modulations = append(command.Modulations, modulation)
			command.Ordered = append(command.Ordered, OrderedOption{Kind: OrderedModulation, Argument: value})
		case "--range":
			override, err := parseRangeOverride(value)
			if err != nil {
				return Command{}, err
			}
			command.RangeOverride = &override
		case "-v":
			gain, err := parseBoundedNumber("gain", value, 0, 1)
			if err != nil {
				return Command{}, err
			}
			command.Gain = &gain
		case "-t":
			trigger, err := control.ParseTrigger(value)
			if err != nil {
				return Command{}, err
			}
			command.Trigger = &trigger
		case "-n":
			notes, err := primitive.ParseMaterial(value)
			if err != nil {
				return Command{}, err
			}
			command.Notes = notes
		case "-r":
			rhythm, err := primitive.ParseRhythm(value)
			if err != nil {
				return Command{}, err
			}
			command.Rhythm = &rhythm
		case "-b":
			bpm, err := primitive.ParseBPM(value)
			if err != nil {
				return Command{}, err
			}
			command.BPM = &bpm
		case "-d":
			duration, err := unit.ParseDuration(value)
			if err != nil {
				return Command{}, err
			}
			command.GateDuration = &duration
		case "-a":
			envelope, err := parseADSR(value)
			if err != nil {
				return Command{}, err
			}
			command.Envelope = &envelope
		case "--swing":
			swing, err := primitive.ParseSwing(value)
			if err != nil {
				return Command{}, err
			}
			command.Swing = &swing
		case "-f":
			effect, err := sound.ParseFilter(value)
			if err != nil {
				return Command{}, err
			}
			command.Effects = append(command.Effects, effect)
			command.Ordered = append(command.Ordered, OrderedOption{Kind: OrderedFilter, Argument: value})
		case "-x":
			effect, err := sound.ParseEffect(value)
			if err != nil {
				return Command{}, err
			}
			command.Effects = append(command.Effects, effect)
			command.Ordered = append(command.Ordered, OrderedOption{Kind: OrderedEffect, Argument: value})
		case "-o":
			if value != "-" {
				return Command{}, fmt.Errorf("invalid output %q: only - is supported", value)
			}
			command.Output = &value
		}
	}

	return command, nil
}

func optionValue(args []string, index *int, option string) (string, error) {
	next := *index + 1
	if next >= len(args) {
		return "", fmt.Errorf("option %s requires a value", option)
	}
	if _, isOption := sourceOptions[args[next]]; isOption {
		return "", fmt.Errorf("option %s requires a value", option)
	}
	if args[next] == "-l" || args[next] == "-i" || args[next] == "-p" {
		return "", fmt.Errorf("option %s requires a value", option)
	}
	if args[next] == "" {
		return "", fmt.Errorf("option %s value must not be empty", option)
	}
	*index = next
	return args[next], nil
}

func parseWaveform(input string) (Waveform, error) {
	waveform := Waveform(input)
	switch waveform {
	case WaveSine, WaveSquare, WaveSaw, WaveTri, WaveNoise:
		return waveform, nil
	default:
		return "", fmt.Errorf("unknown waveform %q: expected sine, square, saw, tri, or noise", input)
	}
}

func parseBoundedNumber(name, input string, minimum, maximum float64) (float64, error) {
	value, err := unit.ParseNumber(input)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, input, err)
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("invalid %s %q: must be between %v and %v", name, input, minimum, maximum)
	}
	return value, nil
}

func parseADSR(input string) (ADSR, error) {
	parts := strings.Split(input, ",")
	if len(parts) != 4 {
		return ADSR{}, fmt.Errorf("invalid ADSR %q: expected ATTACK,DECAY,SUSTAIN,RELEASE", input)
	}
	attack, err := unit.ParseDuration(parts[0])
	if err != nil {
		return ADSR{}, fmt.Errorf("invalid ADSR %q: attack: %w", input, err)
	}
	decay, err := unit.ParseDuration(parts[1])
	if err != nil {
		return ADSR{}, fmt.Errorf("invalid ADSR %q: decay: %w", input, err)
	}
	sustain, err := parseBoundedNumber("ADSR sustain", parts[2], 0, 1)
	if err != nil {
		return ADSR{}, fmt.Errorf("invalid ADSR %q: %w", input, err)
	}
	release, err := unit.ParseDuration(parts[3])
	if err != nil {
		return ADSR{}, fmt.Errorf("invalid ADSR %q: release: %w", input, err)
	}
	return ADSR{Attack: attack, Decay: decay, Sustain: sustain, Release: release}, nil
}

func parseModulation(input string) (Modulation, error) {
	if strings.Count(input, "=") != 1 {
		return Modulation{}, fmt.Errorf("invalid modulation %q: expected [CONTROL:]TARGET=MAP", input)
	}
	left, mappingToken, _ := strings.Cut(input, "=")
	if left == "" || mappingToken == "" {
		return Modulation{}, fmt.Errorf("invalid modulation %q: expected [CONTROL:]TARGET=MAP", input)
	}

	controlName, target := "", left
	if strings.Count(left, ":") > 1 {
		return Modulation{}, fmt.Errorf("invalid modulation %q: expected at most one control separator", input)
	}
	if before, after, found := strings.Cut(left, ":"); found {
		if before == "" || after == "" {
			return Modulation{}, fmt.Errorf("invalid modulation %q: control and target must not be empty", input)
		}
		controlName, target = before, after
	}
	mapping, err := control.ParseMapping(mappingToken)
	if err != nil {
		return Modulation{}, fmt.Errorf("invalid modulation %q: %w", input, err)
	}
	return Modulation{Control: controlName, Target: target, Mapping: mapping}, nil
}

func parseRangeOverride(input string) (RangeOverride, error) {
	if strings.Count(input, "=") > 1 {
		return RangeOverride{}, fmt.Errorf("invalid range override %q: expected [CONTROL=]MIN..MAX", input)
	}
	controlName, rangeToken := "", input
	if before, after, found := strings.Cut(input, "="); found {
		if before == "" || after == "" {
			return RangeOverride{}, fmt.Errorf("invalid range override %q: control and range must not be empty", input)
		}
		controlName, rangeToken = before, after
	}
	if strings.Contains(rangeToken, "...") {
		return RangeOverride{}, fmt.Errorf("invalid range override %q: expected MIN..MAX", input)
	}
	parsed, err := unit.ParseRange(rangeToken)
	if err != nil {
		return RangeOverride{}, fmt.Errorf("invalid range override %q: %w", input, err)
	}
	return RangeOverride{Control: controlName, Range: parsed}, nil
}
