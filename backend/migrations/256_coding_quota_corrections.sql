-- Historical fixes are explicit append-only entries. Never rewrite a request,
-- wallet anchor, imported opening balance or platform reset to hide a discrepancy.
CREATE TABLE coding_quota_reconciliations (
    id TEXT PRIMARY KEY,
    algorithm TEXT NOT NULL CHECK (algorithm = 'same-window-recovery-v1'),
    account_id BIGINT NOT NULL,
    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    note TEXT NOT NULL CHECK (length(note) BETWEEN 1 AND 2000),
    evidence JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE coding_quota_corrections (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reconciliation_id TEXT NOT NULL REFERENCES coding_quota_reconciliations(id),
    request_row_id BIGINT NOT NULL UNIQUE REFERENCES coding_quota_requests(id),
    before_points NUMERIC(24,8) NOT NULL CHECK (before_points > 0),
    delta NUMERIC(24,8) NOT NULL CHECK (delta = -before_points),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX coding_quota_corrections_event ON coding_quota_corrections(reconciliation_id);
CREATE FUNCTION coding_quota_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'coding quota audit records are append-only';
END;
$$;
CREATE TRIGGER coding_quota_reconciliations_immutable BEFORE UPDATE OR DELETE ON coding_quota_reconciliations
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
CREATE TRIGGER coding_quota_corrections_immutable BEFORE UPDATE OR DELETE ON coding_quota_corrections
    FOR EACH ROW EXECUTE FUNCTION coding_quota_audit_append_only();
