package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const personalResetColumns = `id,actor_user_id,target_user_id,purpose,period_start,period_end,note,experiment_baseline::text,created_at`

type personalResetScanner interface{ Scan(...any) error }

func scanPersonalReset(row personalResetScanner) (*service.PersonalQuotaReset, error) {
	var e service.PersonalQuotaReset
	err := row.Scan(&e.ID, &e.ActorUserID, &e.UserID, &e.Purpose, &e.PeriodStart, &e.PeriodEnd, &e.Note, &e.ExperimentBaseline, &e.CreatedAt)
	return &e, err
}

// Only personal usage is reduced. This expression never enters upstream meters.
func personalExperimentUsageSQL(alias string) string {
	return `GREATEST(0,` + alias + `.used_amount-COALESCE((SELECT MAX(pe.experiment_baseline)
 FROM personal_quota_resets pe WHERE pe.target_user_id=` + alias + `.user_id AND pe.purpose='experiment'
 AND ` + alias + `.purpose='experiment' AND pe.period_start=` + alias + `.window_start),0))`
}

func personalQuotas(ctx context.Context, db codingQuotaQueryer, user int64, now time.Time) ([]service.PersonalQuota, error) {
	readings, err := readCodingQuotas(ctx, db, []int64{user}, now)
	if err != nil {
		return nil, err
	}
	out := make([]service.PersonalQuota, 0, 3)
	for _, v := range readings {
		out = append(out, service.PersonalQuota{Purpose: v.Purpose, Used: v.Used, Pending: v.Pending, PeriodStart: v.Start, PeriodEnd: v.End})
	}
	rows, err := db.QueryContext(ctx, `SELECT `+personalExperimentUsageSQL("w")+`::text,w.period,w.window_start FROM user_purpose_allowances w WHERE w.user_id=$1 AND w.purpose='experiment'`, user)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		v := service.PersonalQuota{Purpose: service.PurposeExperiment, Pending: "0"}
		var period string
		if err := rows.Scan(&v.Used, &period, &v.PeriodStart); err != nil {
			return nil, err
		}
		period = service.AllowancePeriod(v.Purpose, period)
		if service.PurposeWindowExpired(period, v.PeriodStart, now) {
			v.Used = "0"
			v.PeriodStart = service.RenewedPurposeWindow(period, v.PeriodStart, now)
		}
		v.PeriodEnd = service.PurposeResetsAt(period, v.PeriodStart)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ReadPersonalQuotaResets(ctx context.Context, user int64) (*service.PersonalQuotaResetView, error) {
	var role string
	err := r.db.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1 AND deleted_at IS NULL`, user).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	}
	if err != nil {
		return nil, err
	}
	if role != "user" {
		return nil, infraerrors.BadRequest("ORDINARY_USER_REQUIRED", "only ordinary users can be reset")
	}
	var now time.Time
	if err := r.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	quotas, err := personalQuotas(ctx, r.db, user, now)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+personalResetColumns+` FROM personal_quota_resets WHERE target_user_id=$1 ORDER BY created_at DESC,id DESC LIMIT 20`, user)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]service.PersonalQuotaReset, 0)
	for rows.Next() {
		e, err := scanPersonalReset(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.PersonalQuotaResetView{Quotas: quotas, Events: events}, nil
}

func (r *UsageMonitorRepository) ResetPersonalQuota(ctx context.Context, in service.PersonalQuotaResetInput) (*service.PersonalQuotaReset, error) {
	in.Note = strings.TrimSpace(in.Note)
	parsed, parseErr := uuid.Parse(in.ID)
	if parseErr != nil || parsed.String() != in.ID || in.ActorUserID <= 0 || in.UserID <= 0 || !in.Confirm ||
		(in.Purpose != "openai" && in.Purpose != "grok" && in.Purpose != "experiment") || in.PeriodEnd.IsZero() ||
		in.Note == "" || utf8.RuneCountInString(in.Note) > 500 {
		return nil, infraerrors.BadRequest("INVALID_PERSONAL_RESET", "confirmation, UUID, purpose, current period and reason (1–500 characters) required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize identical IDs even when they name different target users.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "personal-reset:"+in.ID); err != nil {
		return nil, err
	}
	if err = lockCodingPerson(ctx, tx, in.UserID, in.Purpose); err != nil {
		return nil, err
	}
	var actorRole, targetRole string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, in.ActorUserID).Scan(&actorRole)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && actorRole != "admin") {
		return nil, infraerrors.Forbidden("ADMIN_REQUIRED", "administrator required")
	}
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, in.UserID).Scan(&targetRole)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	}
	if err != nil {
		return nil, err
	}
	if targetRole != "user" {
		return nil, infraerrors.BadRequest("ORDINARY_USER_REQUIRED", "only ordinary users can be reset")
	}
	prior, err := scanPersonalReset(tx.QueryRowContext(ctx, `SELECT `+personalResetColumns+` FROM personal_quota_resets WHERE id=$1`, in.ID))
	if err == nil {
		if prior.ActorUserID != in.ActorUserID || prior.UserID != in.UserID || prior.Purpose != in.Purpose || prior.Note != in.Note || !prior.PeriodEnd.Equal(in.PeriodEnd) {
			return nil, infraerrors.Conflict("RESET_ID_CONFLICT", "reset ID already used for a different operation")
		}
		return prior, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var raw, period string
	var storedStart, now time.Time
	err = tx.QueryRowContext(ctx, `SELECT used_amount::text,period,window_start FROM user_purpose_allowances WHERE user_id=$1 AND purpose=$2 FOR UPDATE`, in.UserID, in.Purpose).Scan(&raw, &period, &storedStart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("WALLET_NOT_FOUND", "purpose wallet not found")
	}
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	quotas, err := personalQuotas(ctx, tx, in.UserID, now)
	if err != nil {
		return nil, err
	}
	var selected *service.PersonalQuota
	for i := range quotas {
		if quotas[i].Purpose == in.Purpose {
			selected = &quotas[i]
			break
		}
	}
	if selected == nil || !selected.PeriodEnd.Equal(in.PeriodEnd) {
		return nil, infraerrors.Conflict("PERSONAL_PERIOD_CHANGED", "quota period changed; refresh and confirm again")
	}
	baseline := "0"
	if in.Purpose == "experiment" && storedStart.Equal(selected.PeriodStart) {
		baseline = raw
	}
	event, err := scanPersonalReset(tx.QueryRowContext(ctx, `INSERT INTO personal_quota_resets(id,actor_user_id,target_user_id,purpose,period_start,period_end,note,experiment_baseline)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+personalResetColumns, in.ID, in.ActorUserID, in.UserID, in.Purpose, selected.PeriodStart, selected.PeriodEnd, in.Note, baseline))
	if err != nil {
		return nil, err
	}
	if in.Purpose != "experiment" {
		_, err = tx.ExecContext(ctx, `INSERT INTO personal_quota_reset_requests(request_row_id,reset_id)
 SELECT id,$1 FROM coding_quota_requests WHERE user_id=$2 AND purpose=$3 AND started_at>=$4 AND started_at<$5
		ON CONFLICT(request_row_id,reset_id) DO NOTHING`, in.ID, in.UserID, in.Purpose, selected.PeriodStart, selected.PeriodEnd)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return event, nil
}
