CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_official_window_user_cost
    ON usage_logs (official_account_id, official_reset_at, created_at)
    INCLUDE (
        user_id,
        actual_cost,
        official_used_percent,
        official_remaining_percent,
        official_observed_at,
        official_source
    )
    WHERE official_account_id IS NOT NULL
      AND official_reset_at IS NOT NULL;
