-- Each purpose wallet stores its own refresh cycle. week is 7 days from
-- window_start. month is the Beijing calendar month. The admin setting
-- purpose_allowance_policy is the control; this column is what requests use.
ALTER TABLE user_purpose_allowances
    ADD COLUMN IF NOT EXISTS period TEXT NOT NULL DEFAULT 'month';

ALTER TABLE user_purpose_allowances DROP CONSTRAINT IF EXISTS user_purpose_allowances_period_check;
ALTER TABLE user_purpose_allowances
    ADD CONSTRAINT user_purpose_allowances_period_check
    CHECK (period IN ('week', 'month'));

UPDATE user_purpose_allowances
SET period = 'week', updated_at = NOW()
WHERE purpose IN ('openai', 'grok');

UPDATE user_purpose_allowances
SET period = 'month', updated_at = NOW()
WHERE purpose = 'experiment';
