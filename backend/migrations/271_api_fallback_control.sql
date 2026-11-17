-- Administrator-only subscription outage standby. No production policies or
-- approval values are seeded. Historical versions and receipts are append-only.
CREATE TABLE api_fallback_policies (
    id TEXT PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES groups(id),
    platform TEXT NOT NULL CHECK (platform IN ('openai','grok')),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 256),
    version BIGINT NOT NULL CHECK (version > 0),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ,
    config JSONB NOT NULL CHECK (jsonb_typeof(config)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (group_id,platform,model)
);
CREATE TABLE api_fallback_policy_versions (
    policy_id TEXT NOT NULL REFERENCES api_fallback_policies(id),
    version BIGINT NOT NULL,
    actor_user_id BIGINT NOT NULL REFERENCES users(id),
    config JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (policy_id,version)
);
CREATE TABLE api_fallback_attempts (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 256),
    policy_id TEXT NOT NULL REFERENCES api_fallback_policies(id),
    policy_version BIGINT NOT NULL,
    policy_snapshot JSONB NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    platform TEXT NOT NULL CHECK (platform IN ('openai','grok')),
    model TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('reserved','sent','settled','unresolved','cancelled')),
    reason TEXT NOT NULL DEFAULT '',
    reserved_usd NUMERIC(30,12) NOT NULL CHECK (reserved_usd>=0),
    reserved_points NUMERIC(30,12) NOT NULL CHECK (reserved_points>=0),
    official_usd NUMERIC(30,12) NOT NULL DEFAULT 0 CHECK (official_usd>=0),
    supplier_usd NUMERIC(30,12) NOT NULL DEFAULT 0 CHECK (supplier_usd>=0),
    points NUMERIC(30,12) NOT NULL DEFAULT 0 CHECK (points>=0),
    usage JSONB,
    receipt_fingerprint TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    admitted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    sent_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(request_id,api_key_id),
    FOREIGN KEY(policy_id,policy_version) REFERENCES api_fallback_policy_versions(policy_id,version)
);
CREATE INDEX api_fallback_attempts_policy_spend ON api_fallback_attempts(policy_id,started_at,state);
CREATE INDEX api_fallback_attempts_person_period ON api_fallback_attempts(user_id,platform,started_at)
    WHERE state IN ('reserved','sent','unresolved');
CREATE INDEX api_fallback_attempts_recent ON api_fallback_attempts(started_at DESC,id DESC);
CREATE INDEX api_fallback_attempts_lineage ON api_fallback_attempts(user_id,api_key_id,model,account_id)
    WHERE state='settled';
CREATE TABLE api_fallback_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    attempt_id TEXT REFERENCES api_fallback_attempts(id),
    policy_id TEXT NOT NULL REFERENCES api_fallback_policies(id),
    actor_user_id BIGINT REFERENCES users(id),
    event_type TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX api_fallback_events_policy ON api_fallback_events(policy_id,created_at DESC,id DESC);
CREATE INDEX api_fallback_events_attempt ON api_fallback_events(attempt_id,id);
CREATE TRIGGER api_fallback_versions_immutable BEFORE UPDATE OR DELETE ON api_fallback_policy_versions
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
CREATE TRIGGER api_fallback_events_immutable BEFORE UPDATE OR DELETE ON api_fallback_events
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
