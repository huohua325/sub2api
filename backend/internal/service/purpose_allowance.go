package service

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	UsageKindCoding     = "coding"
	UsageKindExperiment = "experiment"

	PurposeOpenAI     = "openai"
	PurposeGrok       = "grok"
	PurposeExperiment = "experiment"

	PurposeOpenAILimit     = 100
	PurposeGrokLimit       = 100
	PurposeExperimentLimit = 500

	purposeWeek = 7 * 24 * time.Hour
)

// shanghai has no daylight-saving time, so a fixed offset stays correct
// without zoneinfo on the gateway image.
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// PurposeAllowance is one lab wallet. Coding wallets reset every 7 days.
// The experiment wallet resets at 00:00 on the first day of each Beijing month.
type PurposeAllowance struct {
	UserID      int64
	Purpose     string
	LimitAmount float64
	UsedAmount  float64
	Period      string
	WindowStart time.Time
}

// PurposeAllowanceRule is one use's cap and refresh cycle.
// week resets 7 days after the current window start. month resets at 00:00
// Beijing time on the first day of each month.
type PurposeAllowanceRule struct {
	Limit  float64 `json:"limit"`
	Period string  `json:"period"`
}

// PurposeAllowancePolicy is the lab control for Codex, Grok Build, and API experiments.
// It is separate from per-platform daily/weekly/monthly quotas.
type PurposeAllowancePolicy struct {
	OpenAI     PurposeAllowanceRule `json:"openai"`
	Grok       PurposeAllowanceRule `json:"grok"`
	Experiment PurposeAllowanceRule `json:"experiment"`
}

func DefaultPurposeAllowancePolicy() PurposeAllowancePolicy {
	return PurposeAllowancePolicy{
		OpenAI:     PurposeAllowanceRule{Limit: PurposeOpenAILimit, Period: "week"},
		Grok:       PurposeAllowanceRule{Limit: PurposeGrokLimit, Period: "week"},
		Experiment: PurposeAllowanceRule{Limit: PurposeExperimentLimit, Period: "month"},
	}
}

func (p PurposeAllowancePolicy) Rule(purpose string) (PurposeAllowanceRule, bool) {
	switch purpose {
	case PurposeOpenAI:
		return p.OpenAI, true
	case PurposeGrok:
		return p.Grok, true
	case PurposeExperiment:
		return p.Experiment, true
	default:
		return PurposeAllowanceRule{}, false
	}
}

// NormalizePurposeAllowancePolicy rejects unknown cycles and negative amounts.
// A missing cycle keeps that use's built-in default.
func NormalizePurposeAllowancePolicy(in PurposeAllowancePolicy) (PurposeAllowancePolicy, error) {
	defaults := DefaultPurposeAllowancePolicy()
	rules := []*PurposeAllowanceRule{&in.OpenAI, &in.Grok, &in.Experiment}
	fallbacks := []PurposeAllowanceRule{defaults.OpenAI, defaults.Grok, defaults.Experiment}
	for i, rule := range rules {
		if rule.Period == "" && rule.Limit == 0 {
			*rule = fallbacks[i]
			continue
		}
		if math.IsNaN(rule.Limit) || math.IsInf(rule.Limit, 0) || rule.Limit < 0 {
			return PurposeAllowancePolicy{}, infraerrors.New(http.StatusBadRequest, "INVALID_PURPOSE_ALLOWANCE", "purpose allowance amount must be zero or greater")
		}
		if rule.Period != "week" && rule.Period != "month" {
			return PurposeAllowancePolicy{}, infraerrors.New(http.StatusBadRequest, "INVALID_PURPOSE_ALLOWANCE", "purpose allowance refresh must be week or month")
		}
	}
	return in, nil
}

// NormalizeUsageKind checks the explicit group field. An empty value is the
// experiment pool. Coding is only valid on the OpenAI and Grok platforms,
// which select the Codex and Grok Build wallets.
func NormalizeUsageKind(platform, kind string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = UsageKindExperiment
	}
	if kind != UsageKindCoding && kind != UsageKindExperiment {
		return "", infraerrors.New(http.StatusBadRequest, "INVALID_USAGE_KIND", "usage_kind must be coding or experiment")
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	if kind == UsageKindCoding && platform != PlatformOpenAI && platform != PlatformGrok {
		return "", infraerrors.New(http.StatusBadRequest, "INVALID_USAGE_KIND", "coding usage is only available for OpenAI and Grok groups")
	}
	return kind, nil
}

// PurposeOfGroup maps a lab group to one wallet using the stored usage kind.
// The group name is intentionally ignored.
func PurposeOfGroup(platform, usageKind string) string {
	kind, err := NormalizeUsageKind(platform, usageKind)
	if err != nil {
		return ""
	}
	if kind == UsageKindExperiment {
		if strings.TrimSpace(platform) == "" {
			return ""
		}
		return PurposeExperiment
	}
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformOpenAI:
		return PurposeOpenAI
	case PlatformGrok:
		return PurposeGrok
	default:
		return ""
	}
}

func groupHasPurposeWallet(group *Group) bool {
	if group == nil {
		return false
	}
	return PurposeOfGroup(group.Platform, group.UsageKind) != ""
}

// PurposePeriod is week for the coding wallets and month for API experiments.
func PurposePeriod(purpose string) string {
	switch purpose {
	case PurposeOpenAI, PurposeGrok:
		return "week"
	case PurposeExperiment:
		return "month"
	default:
		return ""
	}
}

// ShanghaiMonthStart is 00:00 on the first day of the Beijing month containing now.
func ShanghaiMonthStart(now time.Time) time.Time {
	return shanghaiMonthStart(now)
}

func shanghaiMonthStart(now time.Time) time.Time {
	local := now.In(shanghai)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, shanghai)
}

func nextShanghaiMonthStart(now time.Time) time.Time {
	start := shanghaiMonthStart(now)
	return time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, shanghai)
}

func allowancePeriod(purpose, period string) string {
	if period == "week" || period == "month" {
		return period
	}
	return PurposePeriod(purpose)
}

// PurposeWindowExpired reports whether used_amount belongs to a finished window.
func PurposeWindowExpired(period string, windowStart, now time.Time) bool {
	if windowStart.IsZero() {
		return true
	}
	switch period {
	case "week":
		return !now.Before(windowStart.Add(purposeWeek))
	case "month":
		return windowStart.Before(shanghaiMonthStart(now))
	default:
		return false
	}
}

// RenewedPurposeWindow is the start to store when the previous window has expired.
func RenewedPurposeWindow(period string, windowStart, now time.Time) time.Time {
	if windowStart.IsZero() {
		if period == "month" {
			return shanghaiMonthStart(now)
		}
		return now
	}
	switch period {
	case "week":
		elapsed := now.Sub(windowStart)
		weeks := int(elapsed / purposeWeek)
		if weeks < 1 {
			weeks = 1
		}
		return windowStart.Add(time.Duration(weeks) * purposeWeek)
	case "month":
		return shanghaiMonthStart(now)
	default:
		return windowStart
	}
}

// PurposeResetsAt is the instant the current window ends.
func PurposeResetsAt(period string, windowStart time.Time) time.Time {
	switch period {
	case "week":
		if windowStart.IsZero() {
			return time.Time{}
		}
		return windowStart.Add(purposeWeek)
	case "month":
		base := windowStart
		if base.IsZero() {
			base = time.Now()
		}
		return nextShanghaiMonthStart(base)
	default:
		return time.Time{}
	}
}

// EffectivePurposeUsage treats an expired window as unused and returns when it resets.
func EffectivePurposeUsage(row PurposeAllowance, now time.Time) (used float64, resetsAt time.Time) {
	period := allowancePeriod(row.Purpose, row.Period)
	start := row.WindowStart
	if PurposeWindowExpired(period, start, now) {
		start = RenewedPurposeWindow(period, start, now)
		return 0, PurposeResetsAt(period, start)
	}
	return row.UsedAmount, PurposeResetsAt(period, start)
}

// PurposeLimit is the starting amount for a wallet.
func PurposeLimit(purpose string) float64 {
	switch purpose {
	case PurposeOpenAI:
		return PurposeOpenAILimit
	case PurposeGrok:
		return PurposeGrokLimit
	case PurposeExperiment:
		return PurposeExperimentLimit
	default:
		return 0
	}
}

// PurposeAllowanceRepository stores the three lab wallets.
type PurposeAllowanceRepository interface {
	List(ctx context.Context, userID int64) ([]PurposeAllowance, error)
	EnsureDefaults(ctx context.Context, userID int64) error
	ApplyPolicy(ctx context.Context, policy PurposeAllowancePolicy) error
}
