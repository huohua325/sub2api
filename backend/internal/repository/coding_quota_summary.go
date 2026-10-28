package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *UsageMonitorRepository) ReadCodingReservationSummaries(ctx context.Context, users []int64, now time.Time) ([]service.CodingReservationSummary, error) {
	readings, err := r.ReadCodingQuotas(ctx, users, now)
	if err != nil {
		return nil, err
	}
	out := make([]service.CodingReservationSummary, 0, len(readings))
	for _, v := range readings {
		s := service.CodingReservationSummary{UserID: v.UserID, Purpose: v.Purpose, Settled: v.Used, Pending: v.Pending, Reserved: "0", Corrections: "0", AdmissionMode: "single", Reason: "uncalibrated", Mode: service.CodingControlMode(v.Purpose)}
		err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(estimated_points) FILTER(WHERE started_at>=$3 AND started_at<$4),0)::text,
 COUNT(*) FILTER(WHERE state IN('reserved','sent','handoff')),
 COUNT(*) FILTER(WHERE state='unresolved' AND started_at>=$3 AND started_at<$4)
 FROM coding_quota_reservations WHERE user_id=$1 AND purpose=$2 AND state IN('reserved','sent','handoff','unresolved')`, v.UserID, v.Purpose, v.Start, v.End).Scan(&s.Reserved, &s.Active, &s.Unresolved)
		if err != nil {
			return nil, err
		}
		err = r.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE t.state='pending'),count(*) FILTER(WHERE t.state='failed') FROM coding_quota_billing_tasks t JOIN coding_quota_reservations r ON r.id=t.reservation_id WHERE r.user_id=$1 AND r.purpose=$2 AND t.state<>'applied'`, v.UserID, v.Purpose).Scan(&s.PendingTasks, &s.FailedTasks)
		if err != nil {
			return nil, err
		}
		err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(c.delta),0)::text FROM coding_quota_corrections c JOIN coding_quota_requests q ON q.id=c.request_row_id WHERE q.user_id=$1 AND q.purpose=$2 AND q.started_at>=$3 AND q.started_at<$4`, v.UserID, v.Purpose, v.Start, v.End).Scan(&s.Corrections)
		if err != nil {
			return nil, err
		}
		err = r.db.QueryRowContext(ctx, `SELECT admission_mode,reason,reliable,COALESCE(rate::text,''),sample_count,observed_at FROM coding_quota_reservations WHERE user_id=$1 AND purpose=$2 ORDER BY started_at DESC,id DESC LIMIT 1`, v.UserID, v.Purpose).Scan(&s.AdmissionMode, &s.Reason, &s.Reliable, &s.Rate, &s.Samples, &s.ObservedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if s.ObservedAt == nil || now.Sub(*s.ObservedAt) > 20*time.Minute || s.Unresolved > 0 {
			s.Reliable = false
			s.AdmissionMode = "single"
			if s.Unresolved > 0 {
				s.Reason = "unresolved_request"
			}
		}
		out = append(out, s)
	}
	return out, nil
}
