-- Personal coding quotas use immutable request times, independently of parents.
-- Opening balances preserve the old estimate; future charges use increments.
CREATE OR REPLACE FUNCTION pg_temp.coding_quota_timestamp(value TEXT) RETURNS TIMESTAMPTZ
LANGUAGE plpgsql AS $$
BEGIN RETURN value::timestamptz;
EXCEPTION WHEN invalid_datetime_format OR datetime_field_overflow THEN RETURN NULL;
END;
$$;
CREATE TABLE coding_quota_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id TEXT NOT NULL,
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    purpose TEXT NOT NULL CHECK (purpose IN ('openai','grok','experiment','other')),
    account_id BIGINT,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    official_window_end TIMESTAMPTZ,
    weight NUMERIC(28,12) NOT NULL CHECK (weight >= 0),
    points NUMERIC(24,8) NOT NULL DEFAULT 0 CHECK (points >= 0),
    estimated_points NUMERIC(24,8) NOT NULL DEFAULT 0 CHECK (estimated_points >= 0),
    state TEXT NOT NULL CHECK (state IN ('pending','settled','unmetered','imported')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (request_id, api_key_id)
);
CREATE INDEX coding_quota_requests_personal ON coding_quota_requests(user_id,purpose,started_at)
    INCLUDE(points,estimated_points,state,updated_at);
CREATE INDEX coding_quota_requests_pending ON coding_quota_requests(account_id,completed_at,id)
    WHERE state = 'pending';

CREATE TABLE coding_quota_observations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id BIGINT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('openai','grok')),
    window_end TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    used NUMERIC(16,8) NOT NULL CHECK (used >= 0 AND used <= 100),
    source TEXT NOT NULL,
    processed BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE(account_id,window_end,observed_at,used)
);
CREATE INDEX coding_quota_observations_queue ON coding_quota_observations(observed_at,id) WHERE NOT processed;

CREATE TABLE coding_quota_meters (
    account_id BIGINT PRIMARY KEY,
    purpose TEXT NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    used NUMERIC(16,8) NOT NULL,
    points_per_weight NUMERIC(28,12),
    anomaly_at TIMESTAMPTZ,
    anomaly_evidence JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE coding_quota_reset_events (
    id TEXT PRIMARY KEY,
    effective_at TIMESTAMPTZ NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status TEXT NOT NULL CHECK(status IN ('candidate','confirmed')),
    source TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '{}',
    actor_user_id BIGINT REFERENCES users(id),
    confirmed_at TIMESTAMPTZ
);
CREATE INDEX coding_quota_confirmed_resets ON coding_quota_reset_events(effective_at DESC) WHERE status='confirmed';

CREATE TABLE experiment_quota_charges (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id TEXT NOT NULL,
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    account_id BIGINT NOT NULL,
    upstream_request_id TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    amount NUMERIC(20,8),
    billing_effects JSONB NOT NULL DEFAULT '{}',
    state TEXT NOT NULL CHECK(state IN('pending','settled','unresolved')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(request_id,api_key_id)
);
CREATE INDEX experiment_quota_charges_queue ON experiment_quota_charges(next_retry_at,id) WHERE state='pending';
CREATE INDEX experiment_quota_charges_personal ON experiment_quota_charges(user_id,started_at) WHERE state<>'settled';

-- Persist an existing trustworthy pre-request baseline, including its timestamp.
WITH raw AS (
 SELECT id,platform,
 CASE WHEN platform='openai' THEN extra->>'codex_7d_used_percent' ELSE extra#>>'{grok_billing_snapshot,usage_percent}' END p,
 CASE WHEN platform='openai' THEN extra->>'codex_7d_reset_at' ELSE extra#>>'{grok_billing_snapshot,period_end}' END r,
 CASE WHEN platform='openai' THEN extra->>'codex_usage_updated_at'
 ELSE COALESCE(extra#>>'{grok_billing_snapshot,weekly_updated_at}',extra#>>'{grok_billing_snapshot,updated_at}',extra#>>'{grok_billing_snapshot,fetched_at}') END t
 FROM accounts WHERE deleted_at IS NULL AND parent_account_id IS NULL AND platform IN('openai','grok')
)
INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used)
SELECT id,platform,pg_temp.coding_quota_timestamp(r),pg_temp.coding_quota_timestamp(t),p::numeric FROM raw
WHERE CASE WHEN p ~ '^\d+(\.\d+)?$' THEN p::numeric BETWEEN 0 AND 100 ELSE FALSE END
AND pg_temp.coding_quota_timestamp(r)>NOW() AND pg_temp.coding_quota_timestamp(t) IS NOT NULL;

-- Preserve the previous point estimate as an explicitly labelled opening entry.
-- No historical request is fabricated as a precisely metered charge.
WITH raw AS (
    SELECT id,platform,
        CASE WHEN platform='openai' THEN extra->>'codex_7d_used_percent'
             ELSE extra#>>'{grok_billing_snapshot,usage_percent}' END AS p,
        CASE WHEN platform='openai' THEN extra->>'codex_7d_reset_at'
             ELSE extra#>>'{grok_billing_snapshot,period_end}' END AS r,
        CASE WHEN platform='grok' THEN extra#>>'{grok_billing_snapshot,period_start}' END AS s
    FROM accounts WHERE deleted_at IS NULL AND parent_account_id IS NULL AND platform IN ('openai','grok')
), snapshots AS (
    SELECT id,platform,CASE WHEN p ~ '^\d+(\.\d+)?$' THEN p::numeric END AS used,
        pg_temp.coding_quota_timestamp(r) AS reset_at,pg_temp.coding_quota_timestamp(s) AS period_start
    FROM raw
), costs AS (
    SELECT s.id,s.platform,s.used,ul.user_id,SUM(ul.actual_cost) AS cost,
        SUM(SUM(ul.actual_cost)) OVER(PARTITION BY s.id) AS total
    FROM snapshots s JOIN usage_logs ul ON ul.official_account_id=s.id
        AND ul.created_at >= CASE WHEN s.period_start<s.reset_at AND s.period_start>=s.reset_at-INTERVAL '14 days'
            THEN s.period_start ELSE s.reset_at-INTERVAL '7 days' END AND ul.created_at<s.reset_at
        AND (ul.official_reset_at IS NULL OR ABS(EXTRACT(EPOCH FROM(ul.official_reset_at-s.reset_at)))<=300)
    WHERE s.reset_at>NOW() AND s.used BETWEEN 0 AND 100
    GROUP BY s.id,s.platform,s.used,ul.user_id
)
INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,started_at,completed_at,weight,points,state)
SELECT 'opening:'||user_id||':'||platform,0,user_id,platform,NOW(),NOW(),0,
    SUM(ROUND(cost/NULLIF(total,0)*used,4)),'imported'
FROM costs WHERE total>0 GROUP BY user_id,platform
ON CONFLICT DO NOTHING;

-- Snapshot existing clocks; starting the new ledger must not restart a week.
INSERT INTO user_purpose_allowances(user_id,purpose,limit_amount,used_amount,period,window_start)
SELECT u.id,p.purpose,100,0,'week',u.created_at
FROM users u CROSS JOIN(VALUES('openai'),('grok')) p(purpose)
WHERE u.deleted_at IS NULL ON CONFLICT DO NOTHING;

-- 80 was the old shipped Grok default. Back up that value before upgrading it.
INSERT INTO settings(key,value,updated_at)
SELECT 'quota_ledger_previous_point_budget',value,NOW() FROM settings
WHERE key='official_point_budget' ON CONFLICT(key) DO NOTHING;
DO $$
DECLARE budget JSONB;
BEGIN
    SELECT value::jsonb INTO budget FROM settings WHERE key='official_point_budget';
    IF budget->>'grok'='80' THEN
        UPDATE settings SET value=jsonb_set(budget,'{grok}','"30"'::jsonb)::text,updated_at=NOW() WHERE key='official_point_budget';
    END IF;
EXCEPTION WHEN invalid_text_representation THEN
    RAISE WARNING 'Invalid legacy point budget was backed up; application defaults will be used';
END;
$$;
