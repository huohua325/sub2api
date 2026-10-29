package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *UsageMonitorRepository) ReadCodingReservationSummaries(ctx context.Context, users []int64, now time.Time) ([]service.CodingReservationSummary, error) {
	readings, err := r.ReadCodingQuotas(ctx, users, now)
	if err != nil {
		return nil, err
	}
	return r.ReadCodingReservationSummariesForReadings(ctx, readings, now)
}

// ReadCodingReservationSummariesForReadings reuses the caller's personal periods
// and ledger. All users/purposes are aggregated in one database round trip.
func (r *UsageMonitorRepository) ReadCodingReservationSummariesForReadings(ctx context.Context, readings []service.CodingQuotaReading, now time.Time) ([]service.CodingReservationSummary, error) {
	if len(readings) == 0 {
		return []service.CodingReservationSummary{}, nil
	}
	users := make([]int64, 0, len(readings))
	purposes := make([]string, 0, len(readings))
	starts := make([]time.Time, 0, len(readings))
	ends := make([]time.Time, 0, len(readings))
	for _, v := range readings {
		users = append(users, v.UserID)
		purposes = append(purposes, v.Purpose)
		starts = append(starts, v.Start)
		ends = append(ends, v.End)
	}
	rows, err := r.db.QueryContext(ctx, `WITH wanted AS (
 SELECT * FROM unnest($1::bigint[],$2::text[],$4::timestamptz[],$5::timestamptz[]) AS w(user_id,purpose,start_at,end_at)
), reservations AS (
 SELECT w.user_id,w.purpose,
 COALESCE(SUM(r.estimated_points) FILTER(WHERE r.started_at>=w.start_at AND r.started_at<w.end_at),0)::text reserved,
 COUNT(*) FILTER(WHERE r.state IN('reserved','sent','handoff')) active,
 COUNT(*) FILTER(WHERE r.state='unresolved' AND r.started_at>=w.start_at AND r.started_at<w.end_at) unresolved
 FROM wanted w JOIN coding_quota_reservations r USING(user_id,purpose)
 WHERE r.state IN('reserved','sent','handoff','unresolved') GROUP BY w.user_id,w.purpose
), tasks AS (
 SELECT r.user_id,r.purpose,COUNT(*) FILTER(WHERE t.state='pending') pending,COUNT(*) FILTER(WHERE t.state='failed') failed
 FROM coding_quota_billing_tasks t JOIN coding_quota_reservations r ON r.id=t.reservation_id
 JOIN wanted w USING(user_id,purpose) WHERE t.state<>'applied' GROUP BY r.user_id,r.purpose
), corrections AS (
 SELECT w.user_id,w.purpose,SUM(c.delta)::text delta FROM wanted w
 JOIN coding_quota_requests q ON q.user_id=w.user_id AND q.purpose=w.purpose AND q.started_at>=w.start_at AND q.started_at<w.end_at
 JOIN coding_quota_corrections c ON c.request_row_id=q.id GROUP BY w.user_id,w.purpose
), latest AS (
 SELECT DISTINCT ON(r.user_id,r.purpose) r.* FROM coding_quota_reservations r JOIN wanted w USING(user_id,purpose)
 ORDER BY r.user_id,r.purpose,r.started_at DESC,r.id DESC
)
SELECT w.user_id,w.purpose,COALESCE(r.reserved,'0'),COALESCE(r.active,0),COALESCE(r.unresolved,0),
 COALESCE(t.pending,0),COALESCE(t.failed,0),COALESCE(c.delta,'0'),
 COALESCE(l.admission_mode,'single'),COALESCE(l.reason,'uncalibrated'),COALESCE(l.reliable,FALSE),
 COALESCE(l.rate::text,''),COALESCE(l.sample_count,0),`+codingTrustedObservationSQL+`,m.window_end
FROM wanted w LEFT JOIN reservations r USING(user_id,purpose) LEFT JOIN tasks t USING(user_id,purpose)
 LEFT JOIN corrections c USING(user_id,purpose) LEFT JOIN latest l USING(user_id,purpose)
 LEFT JOIN coding_quota_meters m ON m.account_id=l.account_id AND m.purpose=w.purpose
ORDER BY w.user_id,w.purpose`, pq.Array(users), pq.Array(purposes), now, pq.Array(starts), pq.Array(ends))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	byPerson := make(map[struct {
		user    int64
		purpose string
	}]service.CodingQuotaReading, len(readings))
	for _, v := range readings {
		byPerson[struct {
			user    int64
			purpose string
		}{v.UserID, v.Purpose}] = v
	}
	out := make([]service.CodingReservationSummary, 0, len(readings))
	for rows.Next() {
		var s service.CodingReservationSummary
		var end *time.Time
		if err := rows.Scan(&s.UserID, &s.Purpose, &s.Reserved, &s.Active, &s.Unresolved, &s.PendingTasks, &s.FailedTasks, &s.Corrections, &s.AdmissionMode, &s.Reason, &s.Reliable, &s.Rate, &s.Samples, &s.ObservedAt, &end); err != nil {
			return nil, err
		}
		v := byPerson[struct {
			user    int64
			purpose string
		}{s.UserID, s.Purpose}]
		s.Settled, s.Pending, s.Mode = v.Used, v.Pending, service.CodingControlMode(s.Purpose)
		_, maxAge := service.CodingCalibrationLimits()
		if s.ObservedAt == nil || s.ObservedAt.After(now.Add(time.Minute)) || now.Sub(*s.ObservedAt) > maxAge || end == nil || !end.After(now) || s.Unresolved > 0 {
			s.Reliable = false
			s.AdmissionMode = "single"
			switch {
			case s.Unresolved > 0:
				s.Reason = "unresolved_request"
			case end == nil || !end.After(now):
				s.Reason = "window_unknown"
			default:
				s.Reason = "observation_stale"
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
