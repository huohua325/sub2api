-- Personal grants never alter raw requests, meter calibration or cycle anchors.
CREATE TABLE personal_quota_resets (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    actor_user_id BIGINT NOT NULL REFERENCES users(id),
    target_user_id BIGINT NOT NULL REFERENCES users(id),
    purpose TEXT NOT NULL CHECK (purpose IN ('openai','grok','experiment')),
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    note TEXT NOT NULL CHECK (length(btrim(note)) BETWEEN 1 AND 500),
    experiment_baseline NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (experiment_baseline >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (period_end > period_start)
);
CREATE INDEX personal_quota_resets_user_period ON personal_quota_resets
    (target_user_id,purpose,period_start,created_at DESC,id DESC);
CREATE INDEX personal_quota_resets_user_history ON personal_quota_resets
    (target_user_id,created_at DESC,id DESC);
CREATE TABLE personal_quota_reset_requests (
    request_row_id BIGINT NOT NULL REFERENCES coding_quota_requests(id),
    reset_id TEXT NOT NULL REFERENCES personal_quota_resets(id),
    PRIMARY KEY (request_row_id,reset_id)
);
CREATE INDEX personal_quota_reset_requests_event ON personal_quota_reset_requests(reset_id);
CREATE TRIGGER personal_quota_resets_immutable BEFORE UPDATE OR DELETE ON personal_quota_resets
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
CREATE TRIGGER personal_quota_reset_requests_immutable BEFORE UPDATE OR DELETE ON personal_quota_reset_requests
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
