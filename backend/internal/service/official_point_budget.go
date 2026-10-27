package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OfficialPointBudget is one person's official-point cap for a subscription.
// One point is one percent of that parent account's quota.
type OfficialPointBudget struct {
	OpenAI string `json:"openai"`
	Grok   string `json:"grok"`
}

const (
	defaultCodexPointBudget = "50"
	defaultGrokPointBudget  = "30"
	maxOfficialPointBudget  = 10000
)

var officialPointBudgetPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,4})?$`)

func DefaultOfficialPointBudget() OfficialPointBudget {
	return OfficialPointBudget{OpenAI: defaultCodexPointBudget, Grok: defaultGrokPointBudget}
}

// ParseOfficialPointBudget accepts a decimal with at most four places, greater
// than zero and at most 10000. It does not substitute the default for a bad value.
func ParseOfficialPointBudget(raw string) (*big.Rat, error) {
	text := strings.TrimSpace(raw)
	if !officialPointBudgetPattern.MatchString(text) {
		return nil, infraerrors.New(http.StatusBadRequest, "INVALID_OFFICIAL_POINT_BUDGET", "official point budget must be a positive decimal with at most 4 places")
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || value.Sign() <= 0 || value.Cmp(big.NewRat(maxOfficialPointBudget, 1)) > 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "INVALID_OFFICIAL_POINT_BUDGET", "official point budget must be greater than 0 and at most 10000")
	}
	return value, nil
}

// NormalizeOfficialPointBudget fills a missing platform from its own default and
// rejects a present key that is not a valid budget.
func NormalizeOfficialPointBudget(in OfficialPointBudget) (OfficialPointBudget, error) {
	defaults := DefaultOfficialPointBudget()
	if strings.TrimSpace(in.OpenAI) == "" {
		in.OpenAI = defaults.OpenAI
	}
	if strings.TrimSpace(in.Grok) == "" {
		in.Grok = defaults.Grok
	}
	if _, err := ParseOfficialPointBudget(in.OpenAI); err != nil {
		return OfficialPointBudget{}, err
	}
	if _, err := ParseOfficialPointBudget(in.Grok); err != nil {
		return OfficialPointBudget{}, err
	}
	in.OpenAI = strings.TrimSpace(in.OpenAI)
	in.Grok = strings.TrimSpace(in.Grok)
	return in, nil
}

func (s *SettingService) GetOfficialPointBudget(ctx context.Context) OfficialPointBudget {
	fallback := DefaultOfficialPointBudget()
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOfficialPointBudget)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback
	}
	var stored OfficialPointBudget
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		slog.Warn("official_point_budget_invalid", "platform", "json", "reason", "not_decimal")
		return fallback
	}
	return fallbackOfficialPointBudget(stored, fallback)
}

func fallbackOfficialPointBudget(stored, fallback OfficialPointBudget) OfficialPointBudget {
	out := fallback
	if strings.TrimSpace(stored.OpenAI) != "" {
		if _, err := ParseOfficialPointBudget(stored.OpenAI); err != nil {
			slog.Warn("official_point_budget_invalid", "platform", "openai", "reason", officialPointBudgetReason(stored.OpenAI))
		} else {
			out.OpenAI = strings.TrimSpace(stored.OpenAI)
		}
	}
	if strings.TrimSpace(stored.Grok) != "" {
		if _, err := ParseOfficialPointBudget(stored.Grok); err != nil {
			slog.Warn("official_point_budget_invalid", "platform", "grok", "reason", officialPointBudgetReason(stored.Grok))
		} else {
			out.Grok = strings.TrimSpace(stored.Grok)
		}
	}
	return out
}

func officialPointBudgetReason(raw string) string {
	text := strings.TrimSpace(raw)
	if !officialPointBudgetPattern.MatchString(text) {
		if strings.Count(text, ".") == 1 {
			parts := strings.Split(text, ".")
			if len(parts) == 2 && len(parts[1]) > 4 && officialPointBudgetPattern.MatchString(parts[0]) {
				return "too_many_decimals"
			}
		}
		return "not_decimal"
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || value.Sign() <= 0 {
		return "not_positive"
	}
	if value.Cmp(big.NewRat(maxOfficialPointBudget, 1)) > 0 {
		return "above_max"
	}
	return "not_decimal"
}

func (s *SettingService) OfficialPointStudentPercentEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOfficialPointStudentPercent)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

func (b OfficialPointBudget) ForPurpose(purpose string) string {
	switch purpose {
	case PurposeOpenAI:
		return b.OpenAI
	case PurposeGrok:
		return b.Grok
	default:
		return ""
	}
}
