package service

import (
	"context"
	"math"
	"strings"
)

const (
	PlatformQuotaWeekModeRegistration = "registration"
	PlatformQuotaWeekModeMonday       = "monday"
)

func (s *SettingService) GetPlatformQuotaWeekMode(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return PlatformQuotaWeekModeRegistration
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyPlatformQuotaWeekMode)
	if err != nil {
		return PlatformQuotaWeekModeRegistration
	}
	return NormalizePlatformQuotaWeekMode(value)
}

func normalizeInvitationWeeklyLimits(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for platform, weekly := range in {
		if !IsAllowedQuotaPlatform(platform) || weekly < 0 || math.IsNaN(weekly) || math.IsInf(weekly, 0) {
			continue
		}
		out[platform] = weekly
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func overlayInvitationWeeklyQuotas(plan *signupGrantPlan, code *RedeemCode) {
	if plan == nil || code == nil || len(code.PlatformWeeklyLimits) == 0 {
		return
	}
	if plan.PlatformQuotas == nil {
		plan.PlatformQuotas = map[string]*DefaultPlatformQuotaSetting{}
	}
	for platform, weekly := range normalizeInvitationWeeklyLimits(code.PlatformWeeklyLimits) {
		current := plan.PlatformQuotas[platform]
		if current == nil {
			current = &DefaultPlatformQuotaSetting{}
			plan.PlatformQuotas[platform] = current
		}
		value := weekly
		current.WeeklyLimitUSD = &value
	}
}

func NormalizePlatformQuotaWeekMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PlatformQuotaWeekModeMonday:
		return PlatformQuotaWeekModeMonday
	default:
		return PlatformQuotaWeekModeRegistration
	}
}
