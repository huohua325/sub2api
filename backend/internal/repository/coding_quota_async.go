package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *UsageMonitorRepository) StoreCodingAsyncJob(ctx context.Context, job string, user, key int64, v service.GrokVideoPendingBilling) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO coding_quota_async_jobs(job_id,user_id,api_key_id,reservation_id,evidence)
 SELECT $1,$2,$3,$4,$5::jsonb WHERE EXISTS(SELECT 1 FROM coding_quota_reservations WHERE id=$4 AND user_id=$2 AND api_key_id=$3)
 ON CONFLICT(job_id,user_id,api_key_id) DO UPDATE SET evidence=coding_quota_async_jobs.evidence WHERE coding_quota_async_jobs.evidence=EXCLUDED.evidence`, job, user, key, v.CodingReservationID, string(raw))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("async billing evidence conflict")
	}
	return err
}
func (r *UsageMonitorRepository) LoadCodingAsyncJob(ctx context.Context, job string, user, key int64) (*service.GrokVideoPendingBilling, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT evidence FROM coding_quota_async_jobs WHERE job_id=$1 AND user_id=$2 AND api_key_id=$3`, job, user, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v service.GrokVideoPendingBilling
	err = json.Unmarshal(raw, &v)
	return &v, err
}

// CodingAsyncJobHasBilling checks durable ownership, including already applied bills.
func (r *UsageMonitorRepository) CodingAsyncJobHasBilling(ctx context.Context, job string, user, key int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM coding_quota_async_jobs j JOIN coding_quota_billing_tasks t ON t.reservation_id=j.reservation_id AND t.api_key_id=j.api_key_id WHERE j.job_id=$1 AND j.user_id=$2 AND j.api_key_id=$3)`, job, user, key).Scan(&exists)
	return exists, err
}
