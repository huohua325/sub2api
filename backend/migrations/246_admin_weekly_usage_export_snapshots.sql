-- 管理员周用量汇总导出时记下的官方额度快照。
-- 只在导出当前网关周时写入。不随 usage_logs 清理级联删除。
CREATE TABLE IF NOT EXISTS admin_weekly_usage_export_snapshots (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    gateway_week_start TIMESTAMPTZ NOT NULL,
    exported_at TIMESTAMPTZ NOT NULL,
    exported_by_user_id BIGINT NOT NULL,
    platform VARCHAR(50) NOT NULL,
    official_used_percent DOUBLE PRECISION,
    official_reset_at TIMESTAMPTZ,
    official_observed_at TIMESTAMPTZ,
    official_source VARCHAR(40) NOT NULL,
    catalog_cost_since_prev NUMERIC(20,10),
    actual_cost_since_prev NUMERIC(20,10),
    comparable BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_weekly_export_snapshots_account_exported
    ON admin_weekly_usage_export_snapshots (account_id, exported_at DESC);

CREATE INDEX IF NOT EXISTS idx_admin_weekly_export_snapshots_week
    ON admin_weekly_usage_export_snapshots (gateway_week_start, account_id, exported_at DESC);
