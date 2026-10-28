package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

// CodingReservationSummary is administrator-only. Student responses expose only
// ActivityStatus, without points, calibration evidence or request identifiers.
type CodingReservationSummary struct {
	UserID        int64      `json:"user_id"`
	Purpose       string     `json:"purpose"`
	Settled       string     `json:"settled"`
	Pending       string     `json:"pending"`
	Reserved      string     `json:"reserved"`
	Corrections   string     `json:"corrections"`
	Active        int        `json:"active"`
	Unresolved    int        `json:"unresolved"`
	PendingTasks  int        `json:"pending_tasks"`
	FailedTasks   int        `json:"failed_tasks"`
	AdmissionMode string     `json:"admission_mode"`
	Reason        string     `json:"reason"`
	Reliable      bool       `json:"reliable"`
	Rate          string     `json:"rate"`
	Samples       int        `json:"samples"`
	ObservedAt    *time.Time `json:"observed_at"`
	Mode          string     `json:"mode"`
}

func (s *UsageMonitorService) ListCodingReservations(ctx context.Context, q CodingReservationQuery) (CodingReservationPage, error) {
	if s == nil || s.points == nil {
		return CodingReservationPage{}, infraerrors.ServiceUnavailable("CODING_QUOTA_UNAVAILABLE", "quota service unavailable")
	}
	store, ok := s.points.store.(CodingReservationStore)
	if !ok {
		return CodingReservationPage{}, infraerrors.ServiceUnavailable("CODING_QUOTA_UNAVAILABLE", "quota service unavailable")
	}
	return store.ListCodingReservations(ctx, q)
}

func (s *OfficialPointService) applyCodingActivities(ctx context.Context, user int64, now time.Time, cards map[string]OfficialPointResult) {
	store, ok := s.store.(CodingSummaryStore)
	if !ok {
		return
	}
	rows, err := store.ReadCodingReservationSummaries(ctx, []int64{user}, now)
	if err != nil {
		for purpose, card := range cards {
			card.ActivityStatus = "unavailable"
			cards[purpose] = card
		}
		return
	}
	for _, row := range rows {
		card := cards[row.Purpose]
		card.ActivityStatus = row.ActivityStatus()
		cards[row.Purpose] = card
	}
}

type CodingSummaryStore interface {
	ReadCodingReservationSummaries(context.Context, []int64, time.Time) ([]CodingReservationSummary, error)
}

func (v CodingReservationSummary) ActivityStatus() string {
	if v.PendingTasks > 0 {
		return "settling"
	}
	if v.Active > 0 {
		if v.AdmissionMode == "single" && v.Mode == "enforce" {
			return "busy"
		}
		return "processing"
	}
	if v.Unresolved > 0 || v.FailedTasks > 0 {
		return "verifying"
	}
	return "ready"
}
