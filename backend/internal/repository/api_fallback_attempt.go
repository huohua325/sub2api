package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

const fallbackAttemptJSON = `to_jsonb(a)-'policy_snapshot'-'usage'-'receipt_fingerprint'||jsonb_build_object(
 'reserved_usd',a.reserved_usd::text,'reserved_points',a.reserved_points::text,
 'official_usd',a.official_usd::text,'supplier_usd',a.supplier_usd::text,'points',a.points::text,
 'receipt',a.usage,'events',COALESCE((SELECT jsonb_agg(jsonb_build_object('type',e.event_type,'actor_user_id',e.actor_user_id,
 'evidence',e.evidence,'created_at',e.created_at) ORDER BY e.id) FROM api_fallback_events e WHERE e.attempt_id=a.id),'[]'::jsonb))`

func fallbackAttempt(ctx context.Context, tx *sql.Tx, id string) (*service.APIFallbackAttempt, *service.APIFallbackPolicy, string, error) {
	var raw, snapshot []byte
	var fingerprint string
	err := tx.QueryRowContext(ctx, `SELECT `+fallbackAttemptJSON+`,policy_snapshot,receipt_fingerprint FROM api_fallback_attempts a WHERE id=$1 FOR UPDATE`, id).Scan(&raw, &snapshot, &fingerprint)
	if err != nil {
		return nil, nil, "", err
	}
	var a service.APIFallbackAttempt
	var p service.APIFallbackPolicy
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, nil, "", err
	}
	if err = json.Unmarshal(snapshot, &p); err != nil {
		return nil, nil, "", err
	}
	return &a, &p, fingerprint, nil
}

// ResolveAPIFallbackLineage never searches another user's key or exposes hashes.
func (r *UsageMonitorRepository) ResolveAPIFallbackLineage(ctx context.Context, user, key, group int64, model string, hashes []string) (int64, error) {
	if len(hashes) == 0 {
		return 0, nil
	}
	if user <= 0 || key <= 0 || group <= 0 || len(model) > 256 {
		return 0, fallbackInvalid("invalid lineage scope")
	}
	if len(hashes) > 100 {
		// A long subscription conversation needs no paid provenance lookup when
		// this scope has never produced encrypted standby state.
		var hasReceipts bool
		err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_fallback_attempts
 WHERE user_id=$1 AND api_key_id=$2 AND (policy_snapshot->>'group_id')::bigint=$3 AND model=$4 AND state='settled'
 AND jsonb_typeof(usage->'encrypted_digests')='array' AND usage->'encrypted_digests'<>'[]'::jsonb)`, user, key, group, model).Scan(&hasReceipts)
		if err != nil || !hasReceipts {
			return 0, err
		}
		return 0, fallbackInvalid("standby conversation exceeds the verified lineage limit")
	}
	unique := make(map[string]bool)
	for _, h := range hashes {
		if len(h) != 64 {
			return 0, fallbackInvalid("invalid lineage digest")
		}
		if _, err := hex.DecodeString(h); err != nil {
			return 0, fallbackInvalid("invalid lineage digest")
		}
		unique[h] = true
	}
	wanted := make([]string, 0, len(unique))
	for h := range unique {
		wanted = append(wanted, h)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.account_id FROM api_fallback_attempts a
 CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.usage->'encrypted_digests')='array' THEN a.usage->'encrypted_digests' ELSE '[]'::jsonb END) h(digest)
 WHERE a.user_id=$1 AND a.api_key_id=$2 AND (a.policy_snapshot->>'group_id')::bigint=$3 AND a.model=$4 AND a.state='settled' AND h.digest=ANY($5::text[])
 GROUP BY a.account_id HAVING COUNT(DISTINCT h.digest)=$6`, user, key, group, model, pq.Array(wanted), len(wanted))
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var account int64
	count := 0
	for rows.Next() {
		if err = rows.Scan(&account); err != nil {
			return 0, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, nil
	}
	// Row IDs survive credential or endpoint edits. Such an edit must not
	// authorize sending old encrypted state to a different upstream identity.
	var unchanged bool
	err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts credential
 WHERE credential.id=$1 AND credential.deleted_at IS NULL AND credential.updated_at<=(
 SELECT MIN(a.admitted_at) FROM api_fallback_attempts a
 CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.usage->'encrypted_digests')='array' THEN a.usage->'encrypted_digests' ELSE '[]'::jsonb END) h(digest)
 WHERE a.account_id=$1 AND a.user_id=$2 AND a.api_key_id=$3 AND (a.policy_snapshot->>'group_id')::bigint=$4
 AND a.model=$5 AND a.state='settled' AND h.digest=ANY($6::text[])))`, account, user, key, group, model, pq.Array(wanted)).Scan(&unchanged)
	if err != nil {
		return 0, err
	}
	if !unchanged {
		return 0, infraerrors.Conflict("API_FALLBACK_LINEAGE_CHANGED", "standby account changed; start a new conversation")
	}
	return account, nil
}

// lockFallbackAttempt follows the global person -> policy -> attempt lock order.
// Looking up immutable identity before acquiring locks does not lock the row.
func lockFallbackAttempt(ctx context.Context, tx *sql.Tx, id string) error {
	var user int64
	var purpose, policy string
	if err := tx.QueryRowContext(ctx, `SELECT user_id,platform,policy_id FROM api_fallback_attempts WHERE id=$1`, id).Scan(&user, &purpose, &policy); err != nil {
		return err
	}
	if err := lockCodingPerson(ctx, tx, user, purpose); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "api-fallback-policy:"+policy)
	return err
}

func fallbackDecimal(s string) decimal.Decimal { d, _ := decimal.NewFromString(s); return d }

func validateFallbackCostCircuit(ctx context.Context, tx *sql.Tx, p *service.APIFallbackPolicy) error {
	var exceeded bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_fallback_attempts WHERE policy_id=$1 AND (policy_version>=$2 OR completed_at>=$3) AND state='settled' AND reason='cost_bound_exceeded')`, p.ID, p.Version, p.UpdatedAt.UTC()).Scan(&exceeded); err != nil {
		return err
	}
	if exceeded {
		return infraerrors.ServiceUnavailable("API_FALLBACK_COST_BOUND_EXCEEDED", "standby supplier exceeded its approved cost bound; an administrator must review and approve a new policy version")
	}
	return nil
}

func (r *UsageMonitorRepository) ReserveAPIFallback(ctx context.Context, in service.APIFallbackReservationInput) (*service.APIFallbackAttempt, error) {
	if _, err := uuid.Parse(in.ID); err != nil || in.RequestID == "" || len(in.RequestID) > 256 || in.UserID <= 0 || in.APIKeyID <= 0 || in.AccountID <= 0 || in.StartedAt.IsZero() || in.EstimatedInputTokens <= 0 || in.MaxOutputTokens <= 0 {
		return nil, fallbackInvalid("invalid standby admission identity or token bound")
	}
	limit, e := decimal.NewFromString(in.PersonalLimit)
	if e != nil || !limit.IsPositive() {
		return nil, fallbackInvalid("personal point limit is required")
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	in.StartedAt = in.StartedAt.UTC().Truncate(time.Microsecond)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&in.Now); err != nil {
		return nil, err
	}
	if err = lockCodingPerson(ctx, tx, in.UserID, in.Platform); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "api-fallback-policy:"+in.PolicyID); err != nil {
		return nil, err
	}
	if err = requireFallbackAdmin(ctx, tx, in.UserID); err != nil {
		return nil, err
	}
	var keyValid bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys WHERE id=$1 AND user_id=$2 AND group_id=$3 AND deleted_at IS NULL AND status='active'
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND quota<=0 AND rate_limit_5h<=0 AND rate_limit_1d<=0 AND rate_limit_7d<=0)`, in.APIKeyID, in.UserID, in.GroupID).Scan(&keyValid); err != nil {
		return nil, err
	}
	if !keyValid {
		return nil, infraerrors.Forbidden("API_FALLBACK_KEY_CHANGED", "key binding changed")
	}
	p, err := fallbackPolicy(ctx, tx, in.PolicyID)
	if err != nil {
		return nil, err
	}
	if p.ExperimentKeyID > 0 || !p.Enabled || p.Version != in.PolicyVersion || p.GroupID != in.GroupID || p.Platform != in.Platform || p.Model != in.Model || p.ExpiresAt == nil || !p.ExpiresAt.After(in.Now) || !p.CapabilitiesVerified || !containsFallbackID(p.BackupAccountIDs, in.AccountID) {
		return nil, fallbackConflict()
	}
	if err = validateFallbackMembers(ctx, tx, p); err != nil {
		return nil, err
	}
	if err = validateFallbackSelectedBackup(ctx, tx, p, in.AccountID); err != nil {
		return nil, err
	}
	if err = validateFallbackCostCircuit(ctx, tx, p); err != nil {
		return nil, err
	}
	if in.EstimatedInputTokens > p.MaxInputTokens || in.MaxOutputTokens > p.MaxOutputTokens {
		return nil, fallbackInvalid("request exceeds verified token limits")
	}
	old, _, _, err := fallbackAttempt(ctx, tx, in.ID)
	if err == nil {
		if old.UserID != in.UserID || old.APIKeyID != in.APIKeyID || old.RequestID != in.RequestID || old.PolicyID != in.PolicyID || old.PolicyVersion != in.PolicyVersion || old.AccountID != in.AccountID || !old.StartedAt.Equal(in.StartedAt) || old.State != "reserved" {
			return nil, fallbackConflict()
		}
		return old, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	usage, err := readAPIFallbackAdmissionUsage(ctx, tx, in.UserID, in.Platform, in.StartedAt, in.Now)
	if err != nil {
		return nil, err
	}
	if usage.Uncertain {
		return nil, infraerrors.ServiceUnavailable("API_FALLBACK_USAGE_UNKNOWN", "personal usage has unresolved requests")
	}
	var existingPoints string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved_points),0)::text FROM api_fallback_attempts WHERE user_id=$1 AND platform=$2 AND started_at>=$3 AND started_at<$4 AND state IN('reserved','sent','unresolved')`, in.UserID, in.Platform, usage.PeriodStart, usage.PeriodEnd).Scan(&existingPoints)
	if err != nil {
		return nil, err
	}
	official := decimal.NewFromInt(in.EstimatedInputTokens).Mul(fallbackDecimal(p.InputPricePerMillion)).Add(decimal.NewFromInt(in.MaxOutputTokens).Mul(fallbackDecimal(p.OutputPricePerMillion))).Div(decimal.NewFromInt(1000000)).RoundCeil(12)
	cost := official.Mul(fallbackDecimal(p.SupplierMultiplier)).RoundCeil(12)
	points := official.Mul(fallbackDecimal(p.PointsPerUSD)).RoundCeil(8)
	if !cost.IsPositive() || !points.IsPositive() {
		return nil, fallbackInvalid("invalid paid cost estimate")
	}
	if fallbackDecimal(usage.Used).Add(fallbackDecimal(usage.Pending)).Add(fallbackDecimal(usage.Reserved)).Add(fallbackDecimal(existingPoints)).Add(points).GreaterThan(limit) {
		return nil, service.ErrCodingQuotaExhausted
	}
	var total, daily string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN state='settled' THEN supplier_usd WHEN state<>'cancelled' THEN reserved_usd ELSE 0 END),0)::text,
 COALESCE(SUM(CASE WHEN state='settled' THEN supplier_usd WHEN state<>'cancelled' THEN reserved_usd ELSE 0 END) FILTER(WHERE admitted_at>=date_trunc('day',$2::timestamptz AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai' AND admitted_at<(date_trunc('day',$2::timestamptz AT TIME ZONE 'Asia/Shanghai')+INTERVAL '1 day') AT TIME ZONE 'Asia/Shanghai'),0)::text FROM api_fallback_attempts WHERE policy_id=$1`, p.ID, in.Now).Scan(&total, &daily)
	if err != nil {
		return nil, err
	}
	if cost.GreaterThan(fallbackDecimal(p.PerRequestBudgetUSD)) || cost.Add(fallbackDecimal(total)).GreaterThan(fallbackDecimal(p.TotalBudgetUSD)) || cost.Add(fallbackDecimal(daily)).GreaterThan(fallbackDecimal(p.DailyBudgetUSD)) {
		return nil, infraerrors.TooManyRequests("API_FALLBACK_BUDGET_EXHAUSTED", "standby spending budget exhausted")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_attempts(id,request_id,policy_id,policy_version,policy_snapshot,user_id,api_key_id,account_id,platform,model,state,reason,reserved_usd,reserved_points,started_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'reserved','subscription_unavailable',$11,$12,$13)`, in.ID, in.RequestID, p.ID, p.Version, string(raw), in.UserID, in.APIKeyID, in.AccountID, p.Platform, p.Model, cost.StringFixed(12), points.StringFixed(8), in.StartedAt.UTC())
	if err != nil {
		return nil, err
	}
	a, _, _, err := fallbackAttempt(ctx, tx, in.ID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_events(attempt_id,policy_id,event_type) VALUES($1,$2,'reserved')`, a.ID, p.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *UsageMonitorRepository) MarkAPIFallbackSent(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockFallbackAttempt(ctx, tx, id); err != nil {
		return err
	}
	a, _, _, err := fallbackAttempt(ctx, tx, id)
	if err != nil {
		return err
	}
	if a.State != "reserved" {
		return fallbackConflict()
	}
	p, err := fallbackPolicy(ctx, tx, a.PolicyID)
	if err != nil {
		return err
	}
	if !p.Enabled || p.Version != a.PolicyVersion || p.ExpiresAt == nil || !p.ExpiresAt.After(time.Now().UTC()) {
		return fallbackConflict()
	}
	if err = requireFallbackAdmin(ctx, tx, a.UserID); err != nil {
		return err
	}
	var keyValid bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys WHERE id=$1 AND user_id=$2 AND group_id=$3 AND deleted_at IS NULL AND status='active'
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND quota<=0 AND rate_limit_5h<=0 AND rate_limit_1d<=0 AND rate_limit_7d<=0)`, a.APIKeyID, a.UserID, p.GroupID).Scan(&keyValid); err != nil {
		return err
	}
	if !keyValid {
		return fallbackConflict()
	}
	if err = validateFallbackMembers(ctx, tx, p); err != nil {
		return err
	}
	if err = validateFallbackSelectedBackup(ctx, tx, p, a.AccountID); err != nil {
		return err
	}
	if err = validateFallbackCostCircuit(ctx, tx, p); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE api_fallback_attempts SET state='sent',sent_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_events(attempt_id,policy_id,event_type) VALUES($1,$2,'sent')`, id, p.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *UsageMonitorRepository) CancelAPIFallbackUnsent(ctx context.Context, id, reason string) error {
	return r.transitionAPIFallback(ctx, id, reason, true)
}

func (r *UsageMonitorRepository) MarkAPIFallbackUnknown(ctx context.Context, id, reason string) error {
	return r.transitionAPIFallback(ctx, id, reason, false)
}

func (r *UsageMonitorRepository) transitionAPIFallback(ctx context.Context, id, reason string, cancel bool) error {
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockFallbackAttempt(ctx, tx, id); err != nil {
		return err
	}
	a, _, _, err := fallbackAttempt(ctx, tx, id)
	if err != nil {
		return err
	}
	state := "unresolved"
	if cancel {
		state = "cancelled"
		if a.State != "reserved" && a.State != "cancelled" {
			return fallbackConflict()
		}
	} else if a.State != "sent" && a.State != "unresolved" {
		return fallbackConflict()
	}
	if a.State == state {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE api_fallback_attempts SET state=$2,reason=$3,updated_at=clock_timestamp() WHERE id=$1`, id, state, reason); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_events(attempt_id,policy_id,event_type,evidence) VALUES($1,$2,$3,$4)`, id, a.PolicyID, state, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// RecoverAPIFallback only classifies abandoned work. It never sends a model
// request, frees an uncertain reservation, or rewrites a settled receipt.
func (r *UsageMonitorRepository) RecoverAPIFallback(ctx context.Context, cutoff time.Time) error {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM api_fallback_attempts WHERE state IN('reserved','sent') AND updated_at<$1 ORDER BY updated_at,id LIMIT 100`, cutoff.UTC())
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) && pg.Code == "42P01" && strings.Contains(pg.Message, "api_fallback_attempts") {
			return nil
		}
		return err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		tx, e := r.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		e = func() error {
			defer func() { _ = tx.Rollback() }()
			if e := lockFallbackAttempt(ctx, tx, id); e != nil {
				return e
			}
			var policy string
			var stale bool
			if e := tx.QueryRowContext(ctx, `SELECT policy_id,state IN('reserved','sent') AND updated_at<$2 FROM api_fallback_attempts WHERE id=$1 FOR UPDATE`, id, cutoff.UTC()).Scan(&policy, &stale); e != nil {
				return e
			}
			if !stale {
				return tx.Commit()
			}
			if _, e := tx.ExecContext(ctx, `UPDATE api_fallback_attempts SET state='unresolved',reason='recovered_after_interruption',updated_at=clock_timestamp() WHERE id=$1`, id); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO api_fallback_events(attempt_id,policy_id,event_type,evidence) VALUES($1,$2,'unresolved','recovered_after_interruption')`, id, policy); e != nil {
				return e
			}
			return tx.Commit()
		}()
		if e != nil {
			return e
		}
	}
	return nil
}

func fallbackReceiptFingerprint(in service.APIFallbackSettlementInput) string {
	// The token receipt is the billing identity. Different JSON key order or a
	// replay timestamp does not change it; original raw usage remains untouched.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d", in.InputTokens, in.CachedInputTokens, in.OutputTokens)))
	return hex.EncodeToString(sum[:])
}

func settleFallbackAttempt(ctx context.Context, tx *sql.Tx, in service.APIFallbackSettlementInput, allowZero bool) (*service.APIFallbackAttempt, error) {
	if in.InputTokens < 0 || in.CachedInputTokens < 0 || in.OutputTokens < 0 || in.CachedInputTokens > in.InputTokens || (!allowZero && in.InputTokens == 0 && in.OutputTokens == 0) || (!json.Valid(in.Usage) && len(in.Usage) > 0) {
		return nil, fallbackInvalid("native usage with valid nonnegative input/cache/output counts is required")
	}
	a, p, oldFingerprint, err := fallbackAttempt(ctx, tx, in.ID)
	if err != nil {
		return nil, err
	}
	fingerprint := fallbackReceiptFingerprint(in)
	if a.State == "settled" {
		if oldFingerprint != fingerprint {
			return nil, fallbackConflict()
		}
		return a, nil
	}
	if a.State != "sent" && a.State != "unresolved" {
		return nil, fallbackConflict()
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	if in.Now.Before(a.StartedAt) {
		in.Now = a.StartedAt
	}
	official := decimal.NewFromInt(in.InputTokens - in.CachedInputTokens).Mul(fallbackDecimal(p.InputPricePerMillion)).Add(decimal.NewFromInt(in.CachedInputTokens).Mul(fallbackDecimal(p.CachedInputPricePerMillion))).Add(decimal.NewFromInt(in.OutputTokens).Mul(fallbackDecimal(p.OutputPricePerMillion))).Div(decimal.NewFromInt(1000000)).Round(12)
	cost := official.Mul(fallbackDecimal(p.SupplierMultiplier)).Round(12)
	points := official.Mul(fallbackDecimal(p.PointsPerUSD)).Round(8)
	if err = recordAPIFallbackCodingCharge(ctx, tx, service.APIFallbackCodingCharge{RequestID: a.RequestID, APIKeyID: a.APIKeyID, UserID: a.UserID, AccountID: a.AccountID, Purpose: a.Platform, Points: points.StringFixed(8), StartedAt: a.StartedAt, CompletedAt: in.Now}); err != nil {
		return nil, err
	}
	reason := "usage_settled"
	if cost.GreaterThan(fallbackDecimal(a.ReservedUSD)) || points.GreaterThan(fallbackDecimal(a.ReservedPoints)) {
		reason = "cost_bound_exceeded"
	}
	usage := in.Usage
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE api_fallback_attempts SET state='settled',reason=$2,official_usd=$3,supplier_usd=$4,points=$5,usage=$6,receipt_fingerprint=$7,completed_at=$8,updated_at=clock_timestamp() WHERE id=$1`, a.ID, reason, official.StringFixed(12), cost.StringFixed(12), points.StringFixed(8), string(usage), fingerprint, in.Now.UTC()); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_events(attempt_id,policy_id,event_type) VALUES($1,$2,$3)`, a.ID, a.PolicyID, reason); err != nil {
		return nil, err
	}
	a, _, _, err = fallbackAttempt(ctx, tx, a.ID)
	return a, err
}

func (r *UsageMonitorRepository) SettleAPIFallback(ctx context.Context, in service.APIFallbackSettlementInput) (*service.APIFallbackAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockFallbackAttempt(ctx, tx, in.ID); err != nil {
		return nil, err
	}
	a, err := settleFallbackAttempt(ctx, tx, in, false)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *UsageMonitorRepository) ResolveAPIFallback(ctx context.Context, in service.APIFallbackResolveInput) (*service.APIFallbackAttempt, error) {
	in.Evidence = strings.TrimSpace(in.Evidence)
	if !in.Confirm || in.Evidence == "" || utf8.RuneCountInString(in.Evidence) > 2000 || (in.Outcome != "usage" && in.Outcome != "not_billed") {
		return nil, fallbackInvalid("confirmed supplier evidence and usage/not_billed outcome are required")
	}
	if in.Outcome == "not_billed" && (in.InputTokens != 0 || in.CachedInputTokens != 0 || in.OutputTokens != 0) {
		return nil, fallbackInvalid("not-billed evidence cannot include usage")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireFallbackAdmin(ctx, tx, in.ActorUserID); err != nil {
		return nil, err
	}
	if err = lockFallbackAttempt(ctx, tx, in.ID); err != nil {
		return nil, err
	}
	a, _, _, err := fallbackAttempt(ctx, tx, in.ID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	if a.State == "settled" {
		var existing bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_fallback_events WHERE attempt_id=$1 AND event_type='admin_resolved' AND detail=$2::jsonb)`, in.ID, string(raw)).Scan(&existing)
		if err != nil {
			return nil, err
		}
		if !existing {
			return nil, fallbackConflict()
		}
		return a, tx.Commit()
	}
	if a.State != "unresolved" {
		return nil, fallbackConflict()
	}
	usage, _ := json.Marshal(map[string]any{"source": "admin_reconciliation", "input_tokens": in.InputTokens, "cached_input_tokens": in.CachedInputTokens, "output_tokens": in.OutputTokens})
	a, err = settleFallbackAttempt(ctx, tx, service.APIFallbackSettlementInput{ID: in.ID, InputTokens: in.InputTokens, CachedInputTokens: in.CachedInputTokens, OutputTokens: in.OutputTokens, Usage: usage, Now: time.Now().UTC()}, in.Outcome == "not_billed")
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO api_fallback_events(attempt_id,policy_id,actor_user_id,event_type,evidence,detail) VALUES($1,$2,$3,'admin_resolved',$4,$5)`, in.ID, a.PolicyID, in.ActorUserID, in.Evidence, string(raw)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}
