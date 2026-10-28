package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/quotaudit"
)

// CodingQuotaAuditSnapshot performs a bounded, consistent, read-only export.
func (r *UsageMonitorRepository) CodingQuotaAuditSnapshot(ctx context.Context, account int64, from, to time.Time) (quotaudit.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return quotaudit.Snapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	s, err := readCodingAuditSnapshot(ctx, tx, account, from, to)
	if err != nil {
		return s, err
	}
	return s, tx.Commit()
}

func readCodingAuditSnapshot(ctx context.Context, tx *sql.Tx, account int64, from, to time.Time) (quotaudit.Snapshot, error) {
	s := quotaudit.Snapshot{AccountID: account, From: from.UTC(), To: to.UTC()}
	if _, err := quotaudit.Build(s); err != nil {
		return s, err
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT json_build_object(
 'observations',(SELECT COALESCE(json_agg(s ORDER BY observed_at,id),'[]') FROM
 (SELECT id,account_id,purpose,window_end,observed_at,used::text,source,processed FROM coding_quota_observations
 WHERE account_id=$1 AND observed_at>=$2 AND observed_at<=$3 ORDER BY observed_at,id LIMIT 200001) s),
 'requests',(SELECT COALESCE(json_agg(s ORDER BY id),'[]') FROM
 (SELECT id,account_id,user_id,purpose,started_at,completed_at,official_window_end,weight::text,points::text,state,updated_at
 FROM coding_quota_requests WHERE account_id=$1 AND completed_at>=$2 AND completed_at<=$3 ORDER BY id LIMIT 200001) s),
 'events',(SELECT COALESCE(json_agg(s ORDER BY effective_at),'[]') FROM
 (SELECT effective_at,status FROM coding_quota_reset_events WHERE status='confirmed' AND effective_at>=$2 AND effective_at<=$3) s)
)`, account, from.UTC(), to.UTC()).Scan(&raw)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(raw, &s)
	if err == nil && (len(s.Observations) > 200000 || len(s.Requests) > 200000) {
		err = fmt.Errorf("audit snapshot exceeds 200000 rows; narrow the interval")
	}
	return s, err
}

// ApplyCodingQuotaAudit accepts only a pinned plan reproduced from current data.
func (r *UsageMonitorRepository) ApplyCodingQuotaAudit(ctx context.Context, plan quotaudit.Plan, digest, actor, note string) (int, error) {
	if len(digest) != 64 || quotaudit.Digest(plan) != digest || strings.TrimSpace(actor) == "" || len(actor) > 128 || strings.TrimSpace(note) == "" || len(note) > 2000 {
		return 0, fmt.Errorf("a pinned plan, operator identity and reason are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCodingMeter(ctx, tx, plan.AccountID); err != nil {
		return 0, err
	}
	snapshot, err := readCodingAuditSnapshot(ctx, tx, plan.AccountID, plan.From, plan.To)
	if err != nil {
		return 0, err
	}
	fresh, err := quotaudit.Build(snapshot)
	if err != nil {
		return 0, err
	}
	if quotaudit.Digest(fresh) != digest {
		return 0, fmt.Errorf("audit evidence changed or plan was edited; create a new read-only preview")
	}
	inserted := 0
	for _, episode := range fresh.Episodes {
		evidence, err := json.Marshal(episode)
		if err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_reconciliations(id,algorithm,account_id,plan_digest,actor,note,evidence)
 VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(id) DO NOTHING`, episode.ID, fresh.Algorithm, fresh.AccountID, digest, actor, note, string(evidence))
		if err != nil {
			return 0, err
		}
		var matches bool
		if err = tx.QueryRowContext(ctx, `SELECT algorithm=$2 AND account_id=$3 AND evidence=$4::jsonb FROM coding_quota_reconciliations WHERE id=$1`, episode.ID, fresh.Algorithm, fresh.AccountID, string(evidence)).Scan(&matches); err != nil {
			return 0, err
		}
		if !matches {
			return 0, fmt.Errorf("conflicting reconciliation identity %s", episode.ID)
		}
		for _, entry := range episode.Entries {
			var row int64
			err = tx.QueryRowContext(ctx, `SELECT id FROM coding_quota_requests WHERE id=$1 AND account_id=$2 AND user_id=$3
 AND purpose=$4 AND state='settled' AND points=$5::numeric FOR UPDATE`, entry.RequestID, fresh.AccountID, entry.UserID, entry.Purpose, entry.Before).Scan(&row)
			if err != nil {
				return 0, fmt.Errorf("original request %d changed: %w", entry.RequestID, err)
			}
			err = tx.QueryRowContext(ctx, `INSERT INTO coding_quota_corrections(reconciliation_id,request_row_id,before_points,delta)
 VALUES($1,$2,$3,$4) ON CONFLICT(request_row_id) DO NOTHING RETURNING id`, episode.ID, entry.RequestID, entry.Before, entry.Delta).Scan(&row)
			if errors.Is(err, sql.ErrNoRows) {
				err = tx.QueryRowContext(ctx, `SELECT reconciliation_id=$2 AND before_points=$3::numeric AND delta=$4::numeric
 FROM coding_quota_corrections WHERE request_row_id=$1`, entry.RequestID, episode.ID, entry.Before, entry.Delta).Scan(&matches)
				if err != nil {
					return 0, err
				}
				if !matches {
					return 0, fmt.Errorf("request %d already has a different correction", entry.RequestID)
				}
			} else if err != nil {
				return 0, err
			} else {
				inserted++
			}
		}
	}
	if inserted > 0 {
		var end time.Time
		err = tx.QueryRowContext(ctx, `SELECT window_end FROM coding_quota_meters WHERE account_id=$1`, plan.AccountID).Scan(&end)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err == nil {
			if err = refreshCodingCalibration(ctx, tx, plan.AccountID, end); err != nil {
				return 0, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}
