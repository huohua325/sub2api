-- 每笔 OpenAI / Grok 网关计费记下当时订阅账号的官方额度。
-- 官方账号是母账号；影子流量的 account_id 仍是实际应答账号。
-- 百分比为空表示这一笔当时没有未过期的官方读数。历史行保持为空。

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS official_account_id BIGINT,
    ADD COLUMN IF NOT EXISTS official_used_percent NUMERIC(10, 4),
    ADD COLUMN IF NOT EXISTS official_remaining_percent NUMERIC(10, 4),
    ADD COLUMN IF NOT EXISTS official_reset_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS official_window_reset BOOLEAN,
    ADD COLUMN IF NOT EXISTS official_observed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS official_source TEXT;

CREATE INDEX IF NOT EXISTS usage_logs_official_account_created_at_idx
    ON usage_logs (official_account_id, created_at)
    WHERE official_account_id IS NOT NULL;
