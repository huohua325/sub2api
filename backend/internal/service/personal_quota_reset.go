package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type PersonalQuotaResetInput struct {
	ID          string    `json:"id"`
	ActorUserID int64     `json:"-"`
	UserID      int64     `json:"-"`
	Purpose     string    `json:"purpose"`
	PeriodEnd   time.Time `json:"period_end"`
	Note        string    `json:"note"`
	Confirm     bool      `json:"confirm"`
}

type PersonalQuotaReset struct {
	ID                 string    `json:"id"`
	ActorUserID        int64     `json:"actor_user_id"`
	UserID             int64     `json:"target_user_id"`
	Purpose            string    `json:"purpose"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	Note               string    `json:"note"`
	ExperimentBaseline string    `json:"experiment_baseline"`
	CreatedAt          time.Time `json:"created_at"`
}

type PersonalQuota struct {
	Purpose     string    `json:"purpose"`
	Used        string    `json:"used"`
	Pending     string    `json:"pending"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

type PersonalQuotaResetView struct {
	Quotas []PersonalQuota      `json:"quotas"`
	Events []PersonalQuotaReset `json:"events"`
}

type PersonalQuotaResetStore interface {
	ReadPersonalQuotaResets(context.Context, int64) (*PersonalQuotaResetView, error)
	ResetPersonalQuota(context.Context, PersonalQuotaResetInput) (*PersonalQuotaReset, error)
}

func (s *UsageMonitorService) PersonalQuotaResets(ctx context.Context, user int64) (*PersonalQuotaResetView, error) {
	store, ok := s.store.(PersonalQuotaResetStore)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("PERSONAL_QUOTA_UNAVAILABLE", "personal quota service unavailable")
	}
	return store.ReadPersonalQuotaResets(ctx, user)
}

func (s *UsageMonitorService) ResetPersonalQuota(ctx context.Context, in PersonalQuotaResetInput) (*PersonalQuotaReset, error) {
	store, ok := s.store.(PersonalQuotaResetStore)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("PERSONAL_QUOTA_UNAVAILABLE", "personal quota service unavailable")
	}
	return store.ResetPersonalQuota(ctx, in)
}
