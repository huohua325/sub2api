package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type fallbackRoleQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listAPIFallbackBackupAccountIDs(ctx context.Context, db fallbackRoleQueryer, group int64) ([]int64, error) {
	if group <= 0 {
		return nil, fallbackInvalid("a valid backup group is required")
	}
	// Backup is a group-wide account role, independent of model, enabled state
	// and expiry. Keep historical selections quarantined too: editing a policy
	// must not silently turn its old paid supplier into ordinary scheduled use.
	// Do not join current membership; a stale scheduler may still hold an account
	// after unbinding, and the last guard before forwarding must still reject it.
	rows, err := db.QueryContext(ctx, `WITH configs AS (
 SELECT config FROM api_fallback_policies WHERE group_id=$1
 UNION ALL
 SELECT v.config FROM api_fallback_policy_versions v
 JOIN api_fallback_policies p ON p.id=v.policy_id WHERE p.group_id=$1
)
 SELECT DISTINCT b.account_id::bigint FROM configs c
 CROSS JOIN LATERAL jsonb_array_elements_text(CASE
 WHEN jsonb_typeof(c.config->'backup_account_ids')='array' THEN c.config->'backup_account_ids'
 ELSE '[]'::jsonb END) b(account_id)
 UNION
 SELECT a.id FROM accounts a WHERE a.extra->'coding_standby_group_ids' @> jsonb_build_array($1::bigint)
 UNION
 SELECT a.id FROM accounts a JOIN account_groups ag ON ag.account_id=a.id
 WHERE ag.group_id=$1 AND CASE WHEN jsonb_typeof(a.extra->'coding_standby_group_ids')='array'
 THEN jsonb_array_length(a.extra->'coding_standby_group_ids') > 0 ELSE false END
 ORDER BY 1`, group)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListAPIFallbackBackupAccountIDs identifies every current or historical backup
// role in a group, including disabled and expired policies.
func (r *UsageMonitorRepository) ListAPIFallbackBackupAccountIDs(ctx context.Context, group int64) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage monitor repository SQL executor is not configured")
	}
	return listAPIFallbackBackupAccountIDs(ctx, r.db, group)
}

// ListAPIFallbackBackupAccountIDs makes the durable backup role available to
// schedulers, model catalogs and the fresh guard before ordinary forwarding.
func (r *accountRepository) ListAPIFallbackBackupAccountIDs(ctx context.Context, group int64) ([]int64, error) {
	if r == nil || r.sql == nil {
		return nil, errors.New("account repository SQL executor is not configured")
	}
	return listAPIFallbackBackupAccountIDs(ctx, r.sql, group)
}

func (r *accountRepository) ListCodingStandbyAccounts(ctx context.Context, group int64) ([]*service.Account, error) {
	if r == nil || r.sql == nil {
		return nil, errors.New("account repository SQL executor is not configured")
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT id FROM accounts
 WHERE platform='openai' AND type='apikey' AND deleted_at IS NULL AND status='active'
 AND extra->'coding_standby_group_ids' @> jsonb_build_array($1::bigint)
 ORDER BY priority,id`, group)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return r.GetByIDs(ctx, ids)
}
