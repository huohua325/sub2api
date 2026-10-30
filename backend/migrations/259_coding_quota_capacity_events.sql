-- Capacity changes are independent of personal allowance/reset events.
-- Production evidence is entered explicitly by an operator, never seeded here.
ALTER TABLE coding_quota_meters ADD COLUMN calibration_started_at TIMESTAMPTZ;

CREATE TABLE coding_quota_capacity_events (
    id TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('openai','grok')),
    effective_at TIMESTAMPTZ NOT NULL,
    before_observed_at TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    before_used NUMERIC(16,8) NOT NULL CHECK (before_used BETWEEN 0 AND 100),
    after_used NUMERIC(16,8) NOT NULL CHECK (after_used BETWEEN 0 AND 100),
    capacity_factor NUMERIC(16,8) NOT NULL CHECK (capacity_factor > 1),
    previous_rate NUMERIC(28,12),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    note TEXT NOT NULL CHECK (length(note) BETWEEN 1 AND 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (before_observed_at < effective_at),
    UNIQUE (account_id,purpose,effective_at)
);
CREATE INDEX coding_quota_capacity_events_account_time
    ON coding_quota_capacity_events(account_id,purpose,effective_at DESC);
CREATE TRIGGER coding_quota_capacity_events_immutable BEFORE UPDATE OR DELETE ON coding_quota_capacity_events
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
