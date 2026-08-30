package sound

import (
	"fmt"
	"strings"
	"time"

	"github.com/zalmo/stash/internal/unit"
)

// EffectKind identifies one ordered effect-chain element.
type EffectKind string

const (
	EffectLowPass  EffectKind = "low-pass"
	EffectHighPass EffectKind = "high-pass"
	EffectDelay    EffectKind = "delay"
	EffectDrive    EffectKind = "drive"
)

const DefaultFilterQ = 0.707

// Effect stores the numeric parameters used by exactly one Kind. Keeping the
// chain as a concrete slice makes command-line order explicit and stable.
type Effect struct {
	Kind EffectKind

	Cutoff float64
	Q      float64

	DelayTime time.Duration
	Feedback  float64
	Mix       float64

	Amount float64
}

// Validate checks the parameters relevant to the effect's kind.
func (effect Effect) Validate() error {
	switch effect.Kind {
	case EffectLowPass, EffectHighPass:
		if err := validateGreaterThanZero("filter cutoff", effect.Cutoff); err != nil {
			return err
		}
		return validateGreaterThanZero("filter Q", effect.Q)
	case EffectDelay:
		if effect.DelayTime <= 0 {
			return fmt.Errorf("invalid delay time: must be greater than zero")
		}
		if err := validateRange("delay feedback", effect.Feedback, 0, 0.95); err != nil {
			return err
		}
		return validateRange("delay mix", effect.Mix, 0, 1)
	case EffectDrive:
		return validateRange("drive amount", effect.Amount, 0, 1)
	default:
		return fmt.Errorf("unknown effect kind %q", effect.Kind)
	}
}

// ParseFilter parses lp:CUTOFF[,Q] or hp:CUTOFF[,Q].
func ParseFilter(input string) (Effect, error) {
	name, arguments, found := strings.Cut(input, ":")
	if !found || name == "" || arguments == "" || strings.Contains(arguments, ":") {
		return Effect{}, fmt.Errorf("invalid filter %q: expected lp:CUTOFF[,Q] or hp:CUTOFF[,Q]", input)
	}
	var kind EffectKind
	switch name {
	case "lp":
		kind = EffectLowPass
	case "hp":
		kind = EffectHighPass
	default:
		return Effect{}, fmt.Errorf("unknown filter %q: expected lp or hp", name)
	}

	parts := strings.Split(arguments, ",")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return Effect{}, fmt.Errorf("invalid filter %q: expected %s:CUTOFF[,Q]", input, name)
	}
	cutoff, err := unit.ParseNumber(parts[0])
	if err != nil {
		return Effect{}, fmt.Errorf("invalid filter %q: cutoff: %w", input, err)
	}
	if err := validateGreaterThanZero("filter cutoff", cutoff); err != nil {
		return Effect{}, fmt.Errorf("invalid filter %q: %w", input, err)
	}
	quality := DefaultFilterQ
	if len(parts) == 2 {
		quality, err = unit.ParseNumber(parts[1])
		if err != nil {
			return Effect{}, fmt.Errorf("invalid filter %q: Q: %w", input, err)
		}
		if err := validateGreaterThanZero("filter Q", quality); err != nil {
			return Effect{}, fmt.Errorf("invalid filter %q: %w", input, err)
		}
	}
	return Effect{Kind: kind, Cutoff: cutoff, Q: quality}, nil
}

// ParseEffect parses delay:TIME,FEEDBACK,MIX or drive:AMOUNT.
func ParseEffect(input string) (Effect, error) {
	name, arguments, found := strings.Cut(input, ":")
	if !found || name == "" || arguments == "" || strings.Contains(arguments, ":") {
		return Effect{}, fmt.Errorf("invalid effect %q: expected delay:TIME,FEEDBACK,MIX or drive:AMOUNT", input)
	}
	switch name {
	case "delay":
		return parseDelay(input, arguments)
	case "drive":
		return parseDrive(input, arguments)
	default:
		return Effect{}, fmt.Errorf("unknown effect %q: expected delay or drive", name)
	}
}

func parseDelay(input, arguments string) (Effect, error) {
	parts := strings.Split(arguments, ",")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Effect{}, fmt.Errorf("invalid delay %q: expected delay:TIME,FEEDBACK,MIX", input)
	}
	delay, err := unit.ParseDuration(parts[0])
	if err != nil {
		return Effect{}, fmt.Errorf("invalid delay %q: time: %w", input, err)
	}
	if delay <= 0 {
		return Effect{}, fmt.Errorf("invalid delay %q: time must be greater than zero", input)
	}
	feedback, err := parseEffectRange("delay feedback", parts[1], 0, 0.95)
	if err != nil {
		return Effect{}, fmt.Errorf("invalid delay %q: %w", input, err)
	}
	mix, err := parseEffectRange("delay mix", parts[2], 0, 1)
	if err != nil {
		return Effect{}, fmt.Errorf("invalid delay %q: %w", input, err)
	}
	return Effect{Kind: EffectDelay, DelayTime: delay, Feedback: feedback, Mix: mix}, nil
}

func parseDrive(input, arguments string) (Effect, error) {
	if strings.Contains(arguments, ",") {
		return Effect{}, fmt.Errorf("invalid drive %q: expected drive:AMOUNT", input)
	}
	amount, err := parseEffectRange("drive amount", arguments, 0, 1)
	if err != nil {
		return Effect{}, fmt.Errorf("invalid drive %q: %w", input, err)
	}
	return Effect{Kind: EffectDrive, Amount: amount}, nil
}

func parseEffectRange(name, input string, minimum, maximum float64) (float64, error) {
	value, err := unit.ParseNumber(input)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if err := validateRange(name, value, minimum, maximum); err != nil {
		return 0, err
	}
	return value, nil
}
