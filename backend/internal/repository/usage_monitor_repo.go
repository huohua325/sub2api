package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// UsageMonitorRepository reads the admin usage monitor. It never selects
// accounts.extra or credentials; official snapshots come from named jsonb keys.
type UsageMonitorRepository struct {
	db *sql.DB
}

func NewUsageMonitorRepository(db *sql.DB) *UsageMonitorRepository {
	return &UsageMonitorRepository{db: db}
}

func (r *UsageMonitorRepository) ready() error {
	if r == nil || r.db == nil {
		return fmt.Errorf("usage monitor repository is not configured")
	}
	return nil
}

func (r *UsageMonitorRepository) ListOfficialParents(ctx context.Context) ([]service.UsageMonitorParentRow, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id,
		       a.name,
		       a.platform,
		       a.extra->>'codex_7d_used_percent',
		       a.extra->>'codex_7d_reset_at',
		       a.extra->>'codex_usage_updated_at',
		       a.extra->>'codex_7d_window_minutes',
		       a.extra->>'codex_7d_reset_after_seconds',
		       a.extra#>>'{grok_billing_snapshot,usage_percent}',
		       a.extra#>>'{grok_billing_snapshot,period_start}',
		       a.extra#>>'{grok_billing_snapshot,period_end}',
		       a.extra#>>'{grok_billing_snapshot,period_type}',
		       a.extra#>>'{grok_billing_snapshot,plan}',
		       a.extra#>>'{grok_billing_snapshot,weekly_updated_at}',
		       a.extra#>>'{grok_billing_snapshot,updated_at}',
		       a.extra#>>'{grok_billing_snapshot,fetched_at}',
		       a.schedulable,
		       a.extra->>'codex_local_reset_at'
		FROM accounts a
		WHERE a.deleted_at IS NULL
		  AND a.parent_account_id IS NULL
		  AND a.platform IN ('openai', 'grok')
		ORDER BY a.id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.UsageMonitorParentRow, 0)
	for rows.Next() {
		var row service.UsageMonitorParentRow
		var codexUsed, codexReset, codexUpdated, codexMinutes, codexAfter sql.NullString
		var grokPercent, grokStart, grokEnd, grokType, grokPlan sql.NullString
		var grokWeekly, grokUpdated, grokFetched sql.NullString
		if err := rows.Scan(
			&row.ID, &row.Name, &row.Platform,
			&codexUsed, &codexReset, &codexUpdated, &codexMinutes, &codexAfter,
			&grokPercent, &grokStart, &grokEnd, &grokType, &grokPlan,
			&grokWeekly, &grokUpdated, &grokFetched,
			&row.Schedulable, &row.CodexLocalResetAt,
		); err != nil {
			return nil, err
		}
		row.CodexUsedPercent = nullStringFromSQL(codexUsed)
		row.CodexResetAt = nullStringFromSQL(codexReset)
		row.CodexUsageUpdatedAt = nullStringFromSQL(codexUpdated)
		row.CodexWindowMinutes = nullStringFromSQL(codexMinutes)
		row.CodexResetAfterSeconds = nullStringFromSQL(codexAfter)
		row.GrokUsagePercent = nullStringFromSQL(grokPercent)
		row.GrokPeriodStart = nullStringFromSQL(grokStart)
		row.GrokPeriodEnd = nullStringFromSQL(grokEnd)
		row.GrokPeriodType = nullStringFromSQL(grokType)
		row.GrokPlan = nullStringFromSQL(grokPlan)
		row.GrokWeeklyUpdatedAt = nullStringFromSQL(grokWeekly)
		row.GrokUpdatedAt = nullStringFromSQL(grokUpdated)
		row.GrokFetchedAt = nullStringFromSQL(grokFetched)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ListStampedParentIDs(ctx context.Context, since time.Time) ([]int64, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT official_account_id
		FROM usage_logs
		WHERE official_account_id IS NOT NULL
		  AND official_reset_at IS NOT NULL
		  AND created_at >= $1
	`, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ListLatestStamps(ctx context.Context, accountIDs []int64, since time.Time) ([]service.UsageMonitorStamp, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ON (official_account_id)
		       official_account_id,
		       official_reset_at,
		       official_used_percent::text,
		       official_remaining_percent::text,
		       official_observed_at,
		       official_source
		FROM usage_logs
		WHERE official_account_id = ANY($1)
		  AND official_reset_at IS NOT NULL
		  AND official_used_percent IS NOT NULL
		  AND created_at >= $2
		ORDER BY official_account_id, created_at DESC, id DESC
	`, pq.Array(accountIDs), since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.UsageMonitorStamp, 0)
	for rows.Next() {
		var stamp service.UsageMonitorStamp
		var remaining, source sql.NullString
		var observed sql.NullTime
		if err := rows.Scan(&stamp.AccountID, &stamp.ResetAt, &stamp.UsedPercent, &remaining, &observed, &source); err != nil {
			return nil, err
		}
		stamp.RemainingPercent = nullStringFromSQL(remaining)
		stamp.ObservedAt = nullTimeFromSQL(observed)
		if source.Valid {
			stamp.Source = source.String
		}
		out = append(out, stamp)
	}
	return out, rows.Err()
}

// The 5-minute ball is inclusive and matches officialResetMoved. A log already
// tagged with this parent but missing official_reset_at still counts when its
// time falls inside the official period. Those rows are this account's usage;
// they are not borrowed from another parent.
const usageMonitorWindowMatch = `
ul.official_account_id = w.parent_id
AND ul.created_at >= w.window_start
AND ul.created_at < w.reset_at
AND (
  ul.official_reset_at IS NULL
  OR (
    ul.official_reset_at >= w.reset_at - ($4::bigint * INTERVAL '1 second')
    AND ul.official_reset_at <= w.reset_at + ($4::bigint * INTERVAL '1 second')
  )
)`

var usageMonitorWindowSQL = `
WITH windows AS (
    SELECT *
    FROM unnest($1::bigint[], $2::timestamptz[], $3::timestamptz[])
        AS w(parent_id, reset_at, window_start)
)
SELECT w.parent_id,
       costs.user_id,
       costs.actual_cost,
       slope.p1,
       slope.observed1,
       slope.p0,
       slope.observed0,
       slope.cost_between
FROM windows w
LEFT JOIN LATERAL (
    SELECT ul.user_id, SUM(ul.actual_cost)::text AS actual_cost
    FROM usage_logs ul
    WHERE ` + usageMonitorWindowMatch + `
    GROUP BY ul.user_id
) costs ON TRUE
LEFT JOIN LATERAL (
    SELECT r1.p1, r1.observed1, r0.p0, r0.observed0, cb.cost_between
    FROM (
        SELECT ul.official_used_percent::text AS p1,
               ul.official_observed_at AS observed1,
               ul.official_used_percent AS p1_num
        FROM usage_logs ul
        WHERE ` + usageMonitorWindowMatch + `
          AND ul.official_observed_at IS NOT NULL
          AND ul.official_used_percent IS NOT NULL
        ORDER BY ul.official_observed_at DESC, ul.created_at DESC, ul.id DESC
        LIMIT 1
    ) r1
    LEFT JOIN LATERAL (
        SELECT ul.official_used_percent::text AS p0,
               ul.official_observed_at AS observed0
        FROM usage_logs ul
        WHERE ` + usageMonitorWindowMatch + `
          AND ul.official_observed_at IS NOT NULL
          AND ul.official_used_percent IS NOT NULL
          AND ul.official_observed_at < r1.observed1
          AND ul.official_used_percent < r1.p1_num
        ORDER BY ul.official_observed_at DESC, ul.created_at DESC, ul.id DESC
        LIMIT 1
    ) r0 ON TRUE
    LEFT JOIN LATERAL (
        SELECT SUM(ul.actual_cost)::text AS cost_between
        FROM usage_logs ul
        WHERE ` + usageMonitorWindowMatch + `
          AND r0.observed0 IS NOT NULL
          AND ul.created_at > r0.observed0
          AND ul.created_at <= r1.observed1
    ) cb ON TRUE
) slope ON TRUE`

func (r *UsageMonitorRepository) ListWindowUsage(ctx context.Context, windows []service.UsageMonitorWindowQuery, resetGap time.Duration) ([]service.UsageMonitorWindowUsage, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if len(windows) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(windows))
	resets := make([]time.Time, len(windows))
	starts := make([]time.Time, len(windows))
	for i, window := range windows {
		ids[i] = window.ParentID
		resets[i] = window.Reset
		starts[i] = window.Start
	}
	gapSeconds := int64(resetGap / time.Second)
	rows, err := r.db.QueryContext(ctx, usageMonitorWindowSQL, pq.Array(ids), pq.Array(resets), pq.Array(starts), gapSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	byParent := map[int64]*service.UsageMonitorWindowUsage{}
	order := make([]int64, 0)
	for rows.Next() {
		var parentID int64
		var userID sql.NullInt64
		var actualCost, p1, p0, costBetween sql.NullString
		var observed1, observed0 sql.NullTime
		if err := rows.Scan(&parentID, &userID, &actualCost, &p1, &observed1, &p0, &observed0, &costBetween); err != nil {
			return nil, err
		}
		usage, ok := byParent[parentID]
		if !ok {
			usage = &service.UsageMonitorWindowUsage{ParentID: parentID, Users: []service.UsageMonitorWindowUserCost{}}
			byParent[parentID] = usage
			order = append(order, parentID)
		}
		if userID.Valid && actualCost.Valid {
			usage.Users = append(usage.Users, service.UsageMonitorWindowUserCost{
				UserID:     userID.Int64,
				ActualCost: actualCost.String,
			})
		}
		if usage.Slope == nil && p1.Valid && observed1.Valid {
			slope := &service.UsageMonitorSlopeRow{P1: p1.String, Observed1: observed1.Time}
			slope.P0 = nullStringFromSQL(p0)
			slope.Observed0 = nullTimeFromSQL(observed0)
			slope.CostBetween = nullStringFromSQL(costBetween)
			usage.Slope = slope
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]service.UsageMonitorWindowUsage, 0, len(order))
	for _, id := range order {
		out = append(out, *byParent[id])
	}
	return out, nil
}

const officialWindowCostSQL = `
WITH windows AS (
    SELECT *
    FROM unnest($1::bigint[], $2::timestamptz[], $3::timestamptz[])
        AS w(parent_id, reset_at, window_start)
)
SELECT w.parent_id,
       COALESCE(SUM(ul.actual_cost), 0)::text AS total_cost,
       COALESCE(SUM(ul.actual_cost) FILTER (WHERE ul.user_id = $5), 0)::text AS user_cost
FROM windows w
LEFT JOIN usage_logs ul ON ` + usageMonitorWindowMatch + `
GROUP BY w.parent_id`

// SumOfficialWindowCost is one indexed aggregate per parent. The user id is
// only inside the aggregate, so the caller does not scan once per user.
func (r *UsageMonitorRepository) SumOfficialWindowCost(ctx context.Context, userID int64, windows []service.UsageMonitorWindowQuery, resetGap time.Duration) ([]service.OfficialWindowCost, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if len(windows) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(windows))
	resets := make([]time.Time, len(windows))
	starts := make([]time.Time, len(windows))
	for i, window := range windows {
		ids[i] = window.ParentID
		resets[i] = window.Reset
		starts[i] = window.Start
	}
	rows, err := r.db.QueryContext(ctx, officialWindowCostSQL, pq.Array(ids), pq.Array(resets), pq.Array(starts), int64(resetGap/time.Second), userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.OfficialWindowCost, 0, len(windows))
	for rows.Next() {
		var row service.OfficialWindowCost
		if err := rows.Scan(&row.ParentID, &row.TotalCost, &row.UserCost); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ListLiveUsers(ctx context.Context) ([]service.UsageMonitorUserRow, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id,
		       u.email,
		       COALESCE(u.username, ''),
		       u.role,
		       w.purpose,
		       w.limit_amount::text,
		       w.used_amount::text,
		       w.period,
		       w.window_start
		FROM users u
		LEFT JOIN user_purpose_allowances w ON w.user_id = u.id
		WHERE u.deleted_at IS NULL
		ORDER BY u.id,
		         CASE w.purpose
		             WHEN 'openai' THEN 1
		             WHEN 'grok' THEN 2
		             WHEN 'experiment' THEN 3
		             ELSE 4
		         END
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.UsageMonitorUserRow, 0)
	for rows.Next() {
		var row service.UsageMonitorUserRow
		var purpose, limitAmount, usedAmount, period sql.NullString
		var windowStart sql.NullTime
		if err := rows.Scan(&row.ID, &row.Email, &row.Username, &row.Role, &purpose, &limitAmount, &usedAmount, &period, &windowStart); err != nil {
			return nil, err
		}
		if purpose.Valid {
			row.HasWallet = true
			row.Purpose = purpose.String
			row.LimitAmount = limitAmount.String
			row.UsedAmount = usedAmount.String
			row.Period = period.String
			if windowStart.Valid {
				row.WindowStart = windowStart.Time
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ListExperimentGroupIDs(ctx context.Context) ([]int64, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT g.id
		FROM groups g
		WHERE (
		    CASE
		        WHEN btrim(g.platform) = '' THEN ''
		        WHEN COALESCE(NULLIF(lower(btrim(g.usage_kind)), ''), 'experiment') = 'experiment'
		            THEN 'experiment'
		        WHEN lower(btrim(g.usage_kind)) = 'coding' AND lower(btrim(g.platform)) = 'openai'
		            THEN 'openai'
		        WHEN lower(btrim(g.usage_kind)) = 'coding' AND lower(btrim(g.platform)) = 'grok'
		            THEN 'grok'
		        ELSE ''
		    END
		) = 'experiment'
		ORDER BY g.id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ListExperimentRequestCounts(ctx context.Context, groupIDs []int64, start, end time.Time) ([]service.UsageMonitorExperimentCount, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ul.user_id, lower(btrim(g.platform)), COUNT(*)
		FROM usage_logs ul
		JOIN groups g ON g.id = ul.group_id
		WHERE ul.group_id = ANY($1)
		  AND ul.created_at >= $2
		  AND ul.created_at < $3
		GROUP BY ul.user_id, lower(btrim(g.platform))
		ORDER BY ul.user_id, 2
	`, pq.Array(groupIDs), start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.UsageMonitorExperimentCount, 0)
	for rows.Next() {
		var count service.UsageMonitorExperimentCount
		var platform sql.NullString
		if err := rows.Scan(&count.UserID, &platform, &count.Count); err != nil {
			return nil, err
		}
		if platform.Valid {
			count.Platform = platform.String
		}
		out = append(out, count)
	}
	return out, rows.Err()
}
