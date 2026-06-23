package privacy

import (
	"errors"
	"slices"
)

const (
	Private   = "private"
	Community = "community"
	Public    = "public"
)

var ErrInvalidTier = errors.New("invalid privacy tier")

func NormalizeTier(tier string) (string, error) {
	if tier == "" {
		return Private, nil
	}
	switch tier {
	case Private, Community, Public:
		return tier, nil
	default:
		return "", ErrInvalidTier
	}
}

func TiersForMode(mode string) ([]string, error) {
	if mode == "" {
		mode = Private
	}
	switch mode {
	case Private:
		return []string{Private, Community, Public}, nil
	case Community:
		return []string{Community, Public}, nil
	case Public:
		return []string{Public}, nil
	default:
		return nil, ErrInvalidTier
	}
}

func NormalizeTiers(tiers []string) ([]string, error) {
	if len(tiers) == 0 {
		return TiersForMode(Private)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		normalized, err := NormalizeTier(tier)
		if err != nil {
			return nil, err
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	slices.Sort(out)
	return out, nil
}

func Accepts(tiers []string, requested string) bool {
	requested, err := NormalizeTier(requested)
	if err != nil {
		return false
	}
	for _, tier := range tiers {
		if tier == requested {
			return true
		}
	}
	return false
}
