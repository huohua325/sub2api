package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

var _ service.APIFallbackStore = (*UsageMonitorRepository)(nil)

type fallbackQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func requireFallbackAdmin(ctx context.Context, db fallbackQueryer, actor int64) error {
	var role string
	if err := db.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active'`, actor).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return infraerrors.Forbidden("API_FALLBACK_ADMIN_REQUIRED", "administrator required")
		}
		return err
	}
	if role != "admin" {
		return infraerrors.Forbidden("API_FALLBACK_ADMIN_REQUIRED", "administrator required")
	}
	return nil
}

func fallbackPolicy(ctx context.Context, db fallbackQueryer, id string) (*service.APIFallbackPolicy, error) {
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT config FROM api_fallback_policies WHERE id=$1`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var p service.APIFallbackPolicy
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func fallbackConflict() error {
	return infraerrors.Conflict("API_FALLBACK_CONFLICT", "standby configuration or request changed; refresh before retrying")
}
func fallbackInvalid(message string) error {
	return infraerrors.BadRequest("INVALID_API_FALLBACK", message)
}

func normalizeFallbackPolicy(p *service.APIFallbackPolicy, now time.Time) error {
	p.CostCircuitOpen = false // Derived from immutable receipts, never supplied by a caller.
	if p.Enabled && strings.TrimSpace(p.CachedInputPricePerMillion) == "" {
		return fallbackInvalid("enabling standby requires an explicitly approved cached input price; use 0 only for verified free cache")
	}
	p.Model = strings.TrimSpace(p.Model)
	p.Note = strings.TrimSpace(p.Note)
	if p.GroupID <= 0 || (p.Platform != "openai" && p.Platform != "grok") || p.Model == "" || len(p.Model) > 256 || strings.ContainsAny(p.Model, "*?\r\n") || p.Note == "" || utf8.RuneCountInString(p.Note) > 1000 {
		return fallbackInvalid("group, exact model and reason (1–1000 characters) are required")
	}
	if len(p.PrimaryAccountIDs) > 100 || len(p.BackupAccountIDs) > 100 || len(p.PrimaryEvidence) > 100 {
		return fallbackInvalid("too many accounts or evidence records")
	}
	seen := make(map[int64]bool)
	for _, ids := range [][]int64{p.PrimaryAccountIDs, p.BackupAccountIDs} {
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return fallbackInvalid("account selections must be distinct positive IDs")
			}
			seen[id] = true
		}
	}
	if p.PrimaryAccountIDs == nil {
		p.PrimaryAccountIDs = []int64{}
	}
	if p.BackupAccountIDs == nil {
		p.BackupAccountIDs = []int64{}
	}
	if p.PrimaryEvidence == nil {
		p.PrimaryEvidence = []service.APIFallbackPrimaryEvidence{}
	}
	evidenceSeen := make(map[int64]bool)
	for _, e := range p.PrimaryEvidence {
		if !containsFallbackID(p.PrimaryAccountIDs, e.AccountID) || evidenceSeen[e.AccountID] || (e.Reason != "subscription_expired" && e.Reason != "quota_exhausted" && e.Reason != "auth_invalid") || e.ObservedAt.IsZero() || e.ObservedAt.After(now.Add(time.Minute)) || !e.ValidUntil.After(e.ObservedAt) || e.ValidUntil.Sub(e.ObservedAt) > 24*time.Hour || strings.TrimSpace(e.Note) == "" || utf8.RuneCountInString(e.Note) > 1000 {
			return fallbackInvalid("primary evidence requires selected account, strict reason, timestamps within 24 hours and a note")
		}
		evidenceSeen[e.AccountID] = true
	}
	fields := []*string{&p.PointsPerUSD, &p.SupplierMultiplier, &p.InputPricePerMillion, &p.CachedInputPricePerMillion, &p.OutputPricePerMillion, &p.PerRequestBudgetUSD, &p.DailyBudgetUSD, &p.TotalBudgetUSD}
	for _, v := range fields {
		if strings.TrimSpace(*v) == "" {
			*v = "0"
		}
		d, err := decimal.NewFromString(*v)
		if err != nil || d.IsNegative() || d.Exponent() < -12 || d.GreaterThan(decimal.NewFromInt(1000000000)) {
			return fallbackInvalid("amounts must be nonnegative decimal strings with at most 12 decimal places")
		}
		*v = d.String()
	}
	if p.MaxInputTokens < 0 || p.MaxInputTokens > 196608 || p.MaxOutputTokens < 0 || p.MaxOutputTokens > 131072 {
		return fallbackInvalid("supported input limit is at most 196608 tokens; output at most 131072")
	}
	if !p.Enabled {
		return nil
	}
	for _, v := range []*string{&p.PointsPerUSD, &p.SupplierMultiplier, &p.InputPricePerMillion, &p.OutputPricePerMillion, &p.PerRequestBudgetUSD, &p.DailyBudgetUSD, &p.TotalBudgetUSD} {
		d, _ := decimal.NewFromString(*v)
		if !d.IsPositive() {
			return fallbackInvalid("enabling standby requires approved positive prices, rates and budgets")
		}
	}
	if !p.CapabilitiesVerified || len(p.PrimaryAccountIDs) == 0 || len(p.BackupAccountIDs) == 0 || p.ExpiresAt == nil || !p.ExpiresAt.After(now) || p.MaxInputTokens == 0 || p.MaxOutputTokens == 0 {
		return fallbackInvalid("enabling standby requires verified capabilities, primary and backup accounts, future expiry and token limits")
	}
	input, _ := decimal.NewFromString(p.InputPricePerMillion)
	cached, _ := decimal.NewFromString(p.CachedInputPricePerMillion)
	if cached.GreaterThan(input) {
		return fallbackInvalid("cached input price must not exceed ordinary input price")
	}
	per, _ := decimal.NewFromString(p.PerRequestBudgetUSD)
	daily, _ := decimal.NewFromString(p.DailyBudgetUSD)
	total, _ := decimal.NewFromString(p.TotalBudgetUSD)
	if per.GreaterThan(daily) || daily.GreaterThan(total) {
		return fallbackInvalid("budgets must satisfy per-request <= daily <= total")
	}
	return nil
}

func containsFallbackID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func validateFallbackMembers(ctx context.Context, tx *sql.Tx, p *service.APIFallbackPolicy) error {
	var group bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND platform='composite' AND usage_kind='coding' AND status='active' AND deleted_at IS NULL)`, p.GroupID).Scan(&group); err != nil {
		return err
	}
	if !group {
		return fallbackInvalid("a unified coding group is required")
	}
	for i, ids := range [][]int64{p.PrimaryAccountIDs, p.BackupAccountIDs} {
		if len(ids) == 0 {
			continue
		}
		typ := "oauth"
		if i == 1 {
			typ = "apikey"
		}
		query := `SELECT COUNT(DISTINCT a.id) FROM accounts a JOIN account_groups ag ON ag.account_id=a.id WHERE a.id=ANY($1::bigint[]) AND a.platform=$2 AND a.type=$3 AND a.deleted_at IS NULL AND ag.group_id=$4`
		if i == 0 {
			// Quota and outage evidence name a distinct upstream parent, even
			// when only credential shadows are assigned to this coding group.
			query = `SELECT COUNT(DISTINCT a.id) FROM accounts a WHERE a.id=ANY($1::bigint[]) AND a.platform=$2 AND a.type=$3 AND a.deleted_at IS NULL AND a.parent_account_id IS NULL
 AND EXISTS(SELECT 1 FROM accounts member JOIN account_groups ag ON ag.account_id=member.id WHERE ag.group_id=$4 AND member.deleted_at IS NULL AND member.platform=$2 AND member.type=$3 AND COALESCE(member.parent_account_id,member.id)=a.id)`
		}
		var count int
		if err := tx.QueryRowContext(ctx, query, pq.Array(ids), p.Platform, typ, p.GroupID).Scan(&count); err != nil {
			return err
		}
		if count != len(ids) {
			return fallbackInvalid("every primary OAuth and backup API account must belong to the selected group and platform")
		}
	}
	return nil
}

func validateFallbackSelectedBackup(ctx context.Context, tx *sql.Tx, p *service.APIFallbackPolicy, account int64) error {
	var ready bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a JOIN account_groups ag ON ag.account_id=a.id
 WHERE a.id=$1 AND a.platform=$2 AND a.type='apikey' AND a.status='active' AND a.schedulable AND a.deleted_at IS NULL AND ag.group_id=$3
 AND a.updated_at<=$4
 AND COALESCE((a.extra->>'quota_limit')::numeric,0)<=0 AND COALESCE((a.extra->>'quota_daily_limit')::numeric,0)<=0 AND COALESCE((a.extra->>'quota_weekly_limit')::numeric,0)<=0)`, account, p.Platform, p.GroupID, p.UpdatedAt).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return fallbackConflict()
	}
	return nil
}

func (r *UsageMonitorRepository) SaveAPIFallbackPolicy(ctx context.Context, in service.APIFallbackPolicyInput) (*service.APIFallbackPolicy, error) {
	if !in.Confirm {
		return nil, fallbackInvalid("explicit confirmation is required")
	}
	p := in.APIFallbackPolicy
	now := time.Now().UTC()
	if err := normalizeFallbackPolicy(&p, now); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireFallbackAdmin(ctx, tx, in.ActorUserID); err != nil {
		return nil, err
	}
	var unifiedGroup bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND platform='composite' AND usage_kind='coding' AND deleted_at IS NULL)`, p.GroupID).Scan(&unifiedGroup); err != nil {
		return nil, err
	}
	if !unifiedGroup {
		return nil, fallbackInvalid("a unified coding group is required")
	}
	if p.Enabled {
		if err = validateFallbackMembers(ctx, tx, &p); err != nil {
			return nil, err
		}
	}
	if p.ID == "" {
		if in.ExpectedVersion != 0 {
			return nil, fallbackConflict()
		}
		p.ID = uuid.NewString()
		p.Version = 1
		p.CreatedAt = now
	} else {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "api-fallback-policy:"+p.ID); err != nil {
			return nil, err
		}
		old, e := fallbackPolicy(ctx, tx, p.ID)
		if e != nil {
			return nil, e
		}
		if old.Version != in.ExpectedVersion || old.GroupID != p.GroupID || old.Platform != p.Platform || old.Model != p.Model {
			return nil, fallbackConflict()
		}
		p.Version = old.Version + 1
		p.CreatedAt = old.CreatedAt
	}
	p.UpdatedAt = now
	p.ApprovedBy = in.ActorUserID
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_policies(id,group_id,platform,model,version,enabled,expires_at,config,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET version=EXCLUDED.version,enabled=EXCLUDED.enabled,expires_at=EXCLUDED.expires_at,config=EXCLUDED.config,updated_at=EXCLUDED.updated_at`, p.ID, p.GroupID, p.Platform, p.Model, p.Version, p.Enabled, p.ExpiresAt, string(raw), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) && pg.Code == "23505" {
			return nil, fallbackConflict()
		}
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_policy_versions(policy_id,version,actor_user_id,config) VALUES($1,$2,$3,$4)`, p.ID, p.Version, in.ActorUserID, string(raw)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_events(policy_id,actor_user_id,event_type,evidence,detail) VALUES($1,$2,'policy_saved',$3,$4)`, p.ID, in.ActorUserID, p.Note, fmt.Sprintf(`{"version":%d,"enabled":%t}`, p.Version, p.Enabled)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *UsageMonitorRepository) DisableAPIFallbackPolicy(ctx context.Context, id string, version, actor int64, note string) (*service.APIFallbackPolicy, error) {
	p, err := fallbackPolicy(ctx, r.db, id)
	if err != nil {
		return nil, err
	}
	p.Enabled = false
	p.Note = note
	return r.SaveAPIFallbackPolicy(ctx, service.APIFallbackPolicyInput{APIFallbackPolicy: *p, ExpectedVersion: version, ActorUserID: actor, Confirm: true})
}

func (r *UsageMonitorRepository) FindAPIFallbackPolicy(ctx context.Context, group int64, platform, model string) (*service.APIFallbackPolicy, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `SELECT id FROM api_fallback_policies WHERE group_id=$1 AND platform=$2 AND model=$3 AND enabled AND expires_at>clock_timestamp()`, group, platform, model).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return fallbackPolicy(ctx, r.db, id)
}

func (r *UsageMonitorRepository) ReadAPIFallback(ctx context.Context, actor int64) (*service.APIFallbackView, error) {
	if err := requireFallbackAdmin(ctx, r.db, actor); err != nil {
		return nil, err
	}
	out := &service.APIFallbackView{Policies: []service.APIFallbackPolicy{}, Attempts: []service.APIFallbackAttempt{}}
	rows, err := r.db.QueryContext(ctx, `SELECT config||jsonb_build_object('cost_circuit_open',EXISTS(SELECT 1 FROM api_fallback_attempts a WHERE a.policy_id=p.id AND (a.policy_version>=p.version OR a.completed_at>=p.updated_at) AND a.state='settled' AND a.reason='cost_bound_exceeded')) FROM api_fallback_policies p ORDER BY created_at DESC,id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var p service.APIFallbackPolicy
		if err = rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &p); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.Policies = append(out.Policies, p)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, `SELECT `+fallbackAttemptJSON+` FROM api_fallback_attempts a ORDER BY started_at DESC,id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var a service.APIFallbackAttempt
		if err = rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.Attempts = append(out.Attempts, a)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(supplier_usd) FILTER(WHERE state='settled'),0)::text,
 COALESCE(SUM(reserved_usd) FILTER(WHERE state IN('reserved','sent')),0)::text,
 COALESCE(SUM(reserved_usd) FILTER(WHERE state='unresolved'),0)::text,
 COALESCE(SUM(CASE WHEN state='settled' THEN supplier_usd WHEN state<>'cancelled' THEN reserved_usd ELSE 0 END) FILTER(WHERE admitted_at>=date_trunc('day',clock_timestamp() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),0)::text,
 COUNT(*) FILTER(WHERE state='settled'),COUNT(*) FILTER(WHERE state='unresolved') FROM api_fallback_attempts`).Scan(&out.Summary.SettledUSD, &out.Summary.ReservedUSD, &out.Summary.UnresolvedUSD, &out.Summary.TodayUSD, &out.Summary.SettledCount, &out.Summary.UnresolvedCount)
	return out, err
}
