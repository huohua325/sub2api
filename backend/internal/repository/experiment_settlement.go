package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var experimentEvidenceHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func recordInitialExperimentSettlement(ctx context.Context, tx *sql.Tx, chargeID int64, cmd *service.UsageBillingCommand, started time.Time) error {
	canonical := fmt.Sprintf("synchronous_billing\n%d\n%s\n%s\n%d\n%.8f", cmd.AccountID, cmd.ExperimentUpstreamRequestID, cmd.RequestID, cmd.APIKeyID, cmd.PurposeCost)
	digest := sha256.Sum256([]byte(canonical))
	var original []byte
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(l)),'[]'::jsonb) FROM
 (SELECT id,input_cost,total_cost,actual_cost FROM usage_logs WHERE request_id=$1 AND api_key_id=$2 FOR UPDATE) l`, cmd.RequestID, cmd.APIKeyID).Scan(&original)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO experiment_quota_settlement_events
 (charge_id,request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,previous_state,original_usage_costs,amount,evidence_source,evidence_sha256)
 VALUES($1,$2,$3,$4,$5,$6,$7,'new',$8::jsonb,$9,'synchronous_billing',$10)`, chargeID, cmd.RequestID, cmd.APIKeyID, cmd.UserID, cmd.AccountID, cmd.ExperimentUpstreamRequestID, started, string(original), cmd.PurposeCost, hex.EncodeToString(digest[:]))
	return err
}

func (r *UsageMonitorRepository) completeExperimentChargeAutomatically(ctx context.Context, id int64, amount float64) error {
	if r == nil || r.db == nil {
		return service.ErrExperimentAccountingUnavailable
	}
	var account int64
	var upstream string
	err := r.db.QueryRowContext(ctx, `SELECT account_id,upstream_request_id FROM experiment_quota_charges WHERE id=$1`, id).Scan(&account, &upstream)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Legacy callers already performed the upstream lookup. New callers should
	// pass the digest of their retained consume-log evidence directly.
	canonical := fmt.Sprintf("automatic_lookup\n%d\n%s\n%.8f", account, upstream, service.QuantizeUsageBillingAmount(amount))
	digest := sha256.Sum256([]byte(canonical))
	return r.CompleteExperimentChargeWithEvidence(ctx, id, amount, service.ExperimentChargeEvidence{
		Source: "automatic_lookup", SHA256: hex.EncodeToString(digest[:]), UpstreamRequestID: upstream, AccountID: account,
	})
}

// CompleteExperimentChargeWithEvidence atomically settles a charge once and
// retains both its prior costs and the immutable upstream evidence identity.
func (r *UsageMonitorRepository) CompleteExperimentChargeWithEvidence(ctx context.Context, id int64, amount float64, evidence service.ExperimentChargeEvidence) error {
	if r == nil || r.db == nil || id <= 0 || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return errors.New("invalid experiment charge")
	}
	if strings.TrimSpace(evidence.Source) == "" || len(evidence.Source) > 96 || !experimentEvidenceHashPattern.MatchString(evidence.SHA256) || evidence.AccountID <= 0 || strings.TrimSpace(evidence.UpstreamRequestID) == "" {
		return errors.New("invalid experiment charge evidence")
	}
	amount = service.QuantizeUsageBillingAmount(amount)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // Commit below is the only successful exit.
	var user, key, account int64
	var request, upstream, state string
	var raw []byte
	var started time.Time
	var previous sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT user_id,started_at,request_id,api_key_id,account_id,billing_effects,upstream_request_id,state,amount
 FROM experiment_quota_charges WHERE id=$1 FOR UPDATE`, id).Scan(&user, &started, &request, &key, &account, &raw, &upstream, &state, &previous)
	if err != nil {
		return err
	}
	if account != evidence.AccountID || upstream != evidence.UpstreamRequestID {
		return errors.New("experiment charge evidence identity conflict")
	}
	if state == "settled" {
		if !previous.Valid || service.QuantizeUsageBillingAmount(previous.Float64) != amount {
			return errors.New("experiment charge amount conflicts with prior settlement")
		}
		return tx.Commit()
	}
	if state != "pending" && state != "unresolved" {
		return errors.New("experiment charge is not eligible for settlement")
	}
	var original []byte
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(l)),'[]'::jsonb) FROM
 (SELECT id,input_cost,total_cost,actual_cost FROM usage_logs WHERE request_id=$1 AND api_key_id=$2 FOR UPDATE) l`, request, key).Scan(&original)
	if err != nil {
		return err
	}
	var inconsistent bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_logs
 WHERE request_id=$1 AND api_key_id=$2 AND actual_cost<>0 AND actual_cost<>$3)`, request, key, amount).Scan(&inconsistent)
	if err != nil {
		return err
	}
	if inconsistent {
		return errors.New("experiment usage cost conflicts with settlement")
	}
	if amount > 0 {
		if err := incrementUsageBillingPurposeAt(ctx, tx, user, service.PurposeExperiment, amount, started); err != nil {
			return err
		}
		var effects service.ExperimentBillingEffects
		if err := json.Unmarshal(raw, &effects); err != nil {
			return err
		}
		if err := applyDeferredExperimentEffects(ctx, tx, key, account, started, amount, effects); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO experiment_quota_settlement_events
 (charge_id,request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,previous_state,previous_amount,original_usage_costs,amount,evidence_source,evidence_sha256)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13)`, id, request, key, user, account, upstream, started, state, previous, string(original), amount, evidence.Source, evidence.SHA256)
	if err != nil {
		return err
	}
	// The original zero-price projection is retained in the append-only event.
	// Existing statistics keep reading their established usage_logs columns.
	_, err = tx.ExecContext(ctx, `UPDATE usage_logs SET input_cost=$3,total_cost=$3,actual_cost=$3
 WHERE request_id=$1 AND api_key_id=$2 AND actual_cost=0`, request, key, amount)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE experiment_quota_charges SET amount=$2,state='settled',updated_at=clock_timestamp() WHERE id=$1`, id, amount)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE experiment_quota_attempts SET state='settled',settled_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE request_id=$1 AND api_key_id=$2 AND user_id=$3 AND account_id=$4 AND state IN ('sent','handed_off','unknown')`, request, key, user, account)
	if err != nil {
		return err
	}
	return tx.Commit()
}
