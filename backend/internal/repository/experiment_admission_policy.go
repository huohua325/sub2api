package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type experimentAdmissionPolicy struct {
	Version              int              `json:"version"`
	UserIDs              []int64          `json:"user_ids"`
	MaxActive            int64            `json:"max_active"`
	MaxOutstanding       int64            `json:"max_outstanding"` // Retained for rollback compatibility.
	MaxPendingAgeSeconds int64            `json:"max_pending_age_seconds"`
	UserLimits           map[string]int64 `json:"user_limits,omitempty"`
}

type experimentQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readExperimentAdmissionPolicy(ctx context.Context, db experimentQueryer, userID int64) (experimentAdmissionPolicy, error) {
	strict := experimentAdmissionPolicy{MaxActive: 1, MaxPendingAgeSeconds: 900}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='experiment_async_admission_policy'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return strict, nil
	}
	if err != nil {
		return strict, err
	}
	policy, err := decodeExperimentAdmissionPolicy(raw)
	if err != nil {
		return strict, err
	}
	for _, id := range policy.UserIDs {
		if id == userID {
			if limit, ok := policy.UserLimits[strconv.FormatInt(userID, 10)]; ok {
				policy.MaxActive = limit
			}
			return policy, nil
		}
	}
	return strict, nil
}

func decodeExperimentAdmissionPolicy(raw string) (experimentAdmissionPolicy, error) {
	var strict experimentAdmissionPolicy
	var policy experimentAdmissionPolicy
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return strict, service.ErrExperimentAccountingUnavailable
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return strict, service.ErrExperimentAccountingUnavailable
	}
	if policy.Version != 1 || policy.MaxActive < 1 || policy.MaxActive > 100 ||
		policy.MaxOutstanding < policy.MaxActive || policy.MaxOutstanding > 1000 ||
		policy.MaxPendingAgeSeconds < 30 || policy.MaxPendingAgeSeconds > 86400 || len(policy.UserIDs) > 1000 {
		return strict, service.ErrExperimentAccountingUnavailable
	}
	for _, id := range policy.UserIDs {
		if id <= 0 {
			return strict, service.ErrExperimentAccountingUnavailable
		}
	}
	if len(policy.UserLimits) > 1000 {
		return strict, service.ErrExperimentAccountingUnavailable
	}
	for id, limit := range policy.UserLimits {
		user, err := strconv.ParseInt(id, 10, 64)
		if err != nil || user <= 0 || strconv.FormatInt(user, 10) != id || limit < 1 || limit > 1000 {
			return strict, service.ErrExperimentAccountingUnavailable
		}
	}
	return policy, nil
}

// The dashboard counts active, pending and exceptional records separately.
// An overdue bill remains eligible for automatic settlement.
const experimentAdmissionCountsSQL = `WITH active AS (
 SELECT request_id,api_key_id,updated_at FROM experiment_quota_attempts
 WHERE user_id=$1 AND state IN ('reserved','sent') AND completed_at IS NULL
), waiting AS (
 SELECT c.request_id,c.api_key_id,c.state,COALESCE(a.completed_at,a.handed_off_at,c.started_at) AS waiting_since,c.updated_at
 FROM experiment_quota_charges c LEFT JOIN experiment_quota_attempts a
 ON a.request_id=c.request_id AND a.api_key_id=c.api_key_id
 WHERE c.user_id=$1 AND c.state IN ('pending','unresolved')
 AND NOT EXISTS(SELECT 1 FROM active a WHERE a.request_id=c.request_id AND a.api_key_id=c.api_key_id)
 UNION ALL
 SELECT a.request_id,a.api_key_id,CASE WHEN a.response_request_id<>'' THEN 'pending' ELSE 'unresolved' END,
 COALESCE(a.completed_at,a.handed_off_at,a.response_recorded_at,a.sent_at,a.created_at),a.updated_at
 FROM experiment_quota_attempts a WHERE a.user_id=$1 AND a.state IN ('sent','handed_off','unknown')
 AND NOT EXISTS(SELECT 1 FROM active x WHERE x.request_id=a.request_id AND x.api_key_id=a.api_key_id)
 AND NOT EXISTS(SELECT 1 FROM experiment_quota_charges c WHERE c.request_id=a.request_id AND c.api_key_id=a.api_key_id)
)
 SELECT (SELECT count(*) FROM active),
 (SELECT count(*) FROM waiting WHERE state='pending' AND waiting_since>$2),
 (SELECT count(*) FROM waiting WHERE state='unresolved' OR waiting_since<=$2),
 (SELECT min(waiting_since) FROM waiting),
 GREATEST((SELECT max(updated_at) FROM waiting),(SELECT max(updated_at) FROM active))`

func readExperimentAdmissionCounts(ctx context.Context, db experimentQueryer, userID int64, overdueBefore time.Time) (service.ExperimentAccountingStatus, error) {
	var out service.ExperimentAccountingStatus
	var oldest, updated sql.NullTime
	err := db.QueryRowContext(ctx, experimentAdmissionCountsSQL, userID, overdueBefore).Scan(
		&out.InFlightRequests, &out.PendingRequests, &out.UnresolvedRequests, &oldest, &updated)
	if oldest.Valid {
		out.OldestPendingAt = &oldest.Time
	}
	if updated.Valid {
		out.UpdatedAt = &updated.Time
	}
	return out, err
}
