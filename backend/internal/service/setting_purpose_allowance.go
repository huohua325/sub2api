package service

import (
	"context"
	"encoding/json"
	"strings"
)

func (s *SettingService) GetPurposeAllowancePolicy(ctx context.Context) (PurposeAllowancePolicy, error) {
	fallback := DefaultPurposeAllowancePolicy()
	if s == nil || s.settingRepo == nil {
		return fallback, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyPurposeAllowancePolicy)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	var policy PurposeAllowancePolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return fallback, nil
	}
	normalized, err := NormalizePurposeAllowancePolicy(policy)
	if err != nil {
		return fallback, nil
	}
	return normalized, nil
}
