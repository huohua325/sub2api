package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReadExperimentAccountingStatus returns one consistent database snapshot of
// current-period unpriced charges and outstanding sends across period boundaries.
func (r *UsageMonitorRepository) ReadExperimentAccountingStatus(ctx context.Context, userID int64, now time.Time) (service.ExperimentAccountingStatus, error) {
	var out service.ExperimentAccountingStatus
	if r == nil || r.db == nil || userID <= 0 || now.IsZero() {
		return out, service.ErrExperimentAccountingUnavailable
	}
	var updated sql.NullTime
	err := r.db.QueryRowContext(ctx, `WITH starts AS (
 SELECT CASE WHEN period='month' THEN date_trunc('month',$2::timestamptz AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'
 ELSE window_start+floor(greatest(extract(epoch FROM($2::timestamptz-window_start)),0)/604800)*interval '168 hours' END AS period_start,
 CASE WHEN period='month' THEN 'month' ELSE 'week' END AS period
 FROM user_purpose_allowances WHERE user_id=$1 AND purpose='experiment'
), bounds AS (
 SELECT period_start,CASE WHEN period='month' THEN ((period_start AT TIME ZONE 'Asia/Shanghai')+interval '1 month') AT TIME ZONE 'Asia/Shanghai'
 ELSE period_start+interval '168 hours' END AS period_end FROM starts
), charges AS (
 SELECT c.* FROM experiment_quota_charges c CROSS JOIN bounds b
 WHERE c.user_id=$1 AND c.started_at>=b.period_start AND c.started_at<b.period_end
), attempts AS (
 SELECT a.* FROM experiment_quota_attempts a WHERE a.user_id=$1 AND a.state IN ('reserved','sent','handed_off','unknown')
)
 SELECT (SELECT count(*) FROM charges WHERE state='pending'),
 (SELECT count(*) FROM charges WHERE state='unresolved'),
 (SELECT count(*) FROM attempts a WHERE NOT EXISTS(SELECT 1 FROM charges c WHERE c.request_id=a.request_id AND c.api_key_id=a.api_key_id AND c.state IN ('pending','unresolved'))),
 GREATEST((SELECT max(updated_at) FROM charges),(SELECT max(updated_at) FROM attempts))
 FROM bounds`, userID, now).Scan(&out.PendingRequests, &out.UnresolvedRequests, &out.InFlightRequests, &updated)
	if err != nil {
		return out, fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	if updated.Valid {
		out.UpdatedAt = &updated.Time
	}
	return out, nil
}
