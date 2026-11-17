-- Unknown GlobalAI charges remain durable and block further unpriced sends.
-- This migration only adds accounting structures; it never resets a wallet.
CREATE TABLE IF NOT EXISTS experiment_quota_attempts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 256),
    api_key_id bigint NOT NULL CHECK (api_key_id > 0),
    user_id bigint NOT NULL CHECK (user_id > 0),
    account_id bigint NOT NULL CHECK (account_id > 0),
    started_at timestamptz NOT NULL,
    state text NOT NULL DEFAULT 'reserved'
        CHECK (state IN ('reserved','sent','handed_off','unknown','released','settled')),
    reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    sent_at timestamptz,
    handed_off_at timestamptz,
    settled_at timestamptz,
    released_at timestamptz,
    response_request_id text NOT NULL DEFAULT '',
    response_status integer CHECK (response_status BETWEEN 100 AND 599),
    response_recorded_at timestamptz,
    UNIQUE (request_id, api_key_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS experiment_quota_attempts_one_open_user
    ON experiment_quota_attempts(user_id)
    WHERE state IN ('reserved','sent','handed_off','unknown');

CREATE INDEX IF NOT EXISTS experiment_quota_attempts_user_started
    ON experiment_quota_attempts(user_id, started_at);

CREATE INDEX IF NOT EXISTS experiment_quota_charges_pending_upstream
    ON experiment_quota_charges(account_id, upstream_request_id)
    INCLUDE (id, attempts) WHERE state='pending';

CREATE INDEX IF NOT EXISTS experiment_quota_charges_pending_account_retry
    ON experiment_quota_charges(account_id, next_retry_at, id) WHERE state='pending';

CREATE TABLE IF NOT EXISTS experiment_quota_settlement_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    charge_id bigint NOT NULL UNIQUE REFERENCES experiment_quota_charges(id),
    request_id text NOT NULL,
    api_key_id bigint NOT NULL,
    user_id bigint NOT NULL,
    account_id bigint NOT NULL,
    upstream_request_id text NOT NULL CHECK (length(upstream_request_id) > 0),
    started_at timestamptz NOT NULL,
    previous_state text NOT NULL,
    previous_amount numeric(20,8),
    original_usage_costs jsonb NOT NULL,
    amount numeric(20,8) NOT NULL CHECK (amount >= 0),
    evidence_source text NOT NULL CHECK (length(evidence_source) BETWEEN 1 AND 96),
    evidence_sha256 text NOT NULL CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS experiment_quota_settlement_events_user_started
    ON experiment_quota_settlement_events(user_id, started_at);

CREATE OR REPLACE FUNCTION prevent_experiment_settlement_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'experiment settlement evidence is append-only';
END;
$$;

DROP TRIGGER IF EXISTS experiment_settlement_events_append_only
    ON experiment_quota_settlement_events;
CREATE TRIGGER experiment_settlement_events_append_only
    BEFORE UPDATE OR DELETE ON experiment_quota_settlement_events
    FOR EACH ROW EXECUTE FUNCTION prevent_experiment_settlement_event_mutation();
