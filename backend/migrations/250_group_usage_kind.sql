-- Lab groups record their quota use explicitly. coding is a programming
-- assistant (OpenAI = Codex, Grok = Grok Build). experiment is the shared
-- monthly API pool. The group name is not consulted.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS usage_kind TEXT NOT NULL DEFAULT 'experiment';

ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_usage_kind_check;
ALTER TABLE groups
    ADD CONSTRAINT groups_usage_kind_check
    CHECK (usage_kind IN ('coding', 'experiment'));

-- Existing lab coding groups. Every other group stays on the experiment default.
UPDATE groups
SET usage_kind = 'coding'
WHERE deleted_at IS NULL
  AND id IN (7, 9);

ALTER TABLE user_purpose_allowances
    ADD COLUMN IF NOT EXISTS window_start TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Keep this week's coding spend. Take the larger of the purpose wallet and the
-- platform weekly counter so the current week is not refunded or double-counted.
UPDATE user_purpose_allowances AS allowance
SET used_amount = GREATEST(allowance.used_amount, quota.weekly_usage_usd),
    window_start = COALESCE(quota.weekly_window_start, allowance.window_start),
    updated_at = NOW()
FROM user_platform_quotas AS quota
WHERE allowance.user_id = quota.user_id
  AND (
        (allowance.purpose = 'openai' AND quota.platform = 'openai')
        OR (allowance.purpose = 'grok' AND quota.platform = 'grok')
      );

-- API experiment resets on the first instant of each Beijing month.
UPDATE user_purpose_allowances
SET window_start = date_trunc('month', NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai',
    updated_at = NOW()
WHERE purpose = 'experiment';
