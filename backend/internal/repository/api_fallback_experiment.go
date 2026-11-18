package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const experimentStandbyKeysQuery = ` FROM api_keys k JOIN groups g ON g.id=k.group_id
 WHERE k.user_id=$1 AND k.deleted_at IS NULL AND k.status='active'
 AND (k.expires_at IS NULL OR k.expires_at>clock_timestamp())
 AND g.deleted_at IS NULL AND g.status='active' AND g.usage_kind='experiment'
 AND g.platform IN ('openai','grok') AND g.subscription_type='standard'`

func experimentStandbyKeyGroup(ctx context.Context, db fallbackQueryer, actor, key int64, platform string) (int64, error) {
	var group int64
	err := db.QueryRowContext(ctx, `SELECT g.id`+experimentStandbyKeysQuery+` AND k.id=$2 AND g.platform=$3`, actor, key, platform).Scan(&group)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fallbackInvalid("select an active API experiment Key belonging to this administrator and platform")
	}
	return group, err
}

func (r *UsageMonitorRepository) listExperimentStandbyKeys(ctx context.Context, actor int64) ([]service.APIFallbackExperimentKey, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT k.id,k.name,g.name,g.platform`+experimentStandbyKeysQuery+` ORDER BY k.id DESC`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []service.APIFallbackExperimentKey{}
	for rows.Next() {
		var key service.APIFallbackExperimentKey
		if err := rows.Scan(&key.ID, &key.Name, &key.GroupName, &key.Platform); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
